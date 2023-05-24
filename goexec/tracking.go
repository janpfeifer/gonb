package goexec

import (
	"github.com/fsnotify/fsnotify"
	"github.com/janpfeifer/gonb/common"
	"github.com/pkg/errors"
	"io/fs"
	"k8s.io/klog/v2"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// This file implements the tracking of files and directories. When updated, these files
// are sent to `gopls` to update its contents for auto-complete and contextual info
// (`InspectRequest`) requests.

// trackingInfo is a substructure of State that holds all the tracking information.
type trackingInfo struct {
	// mu protects tracking information
	mu sync.Mutex

	// tracked files and directories
	tracked map[string]*trackEntry

	// updated is the list of files that changed since last call to State.EnumerateUpdatedFiles.
	updated common.Set[string]

	// watcher for files being tracked. It is notified of file system changes.
	watcher *fsnotify.Watcher

	// go.mod last modification time, used for the AutoTrack
	goModModTime time.Time
}

// trackEntry has information about a file or directory.
type trackEntry struct {
	IsDir          bool
	UpdatedModTime time.Time
}

func newTrackingInfo() *trackingInfo {
	return &trackingInfo{
		tracked: make(map[string]*trackEntry),
		updated: common.MakeSet[string](),
	}
}

// Track adds the fileOrDirPath to the list of tracked files and directories.
// If fileOrDirPath is already tracked, it's a no-op.
func (s *State) Track(fileOrDirPath string) (err error) {
	ti := s.trackingInfo
	ti.mu.Lock()
	defer ti.mu.Unlock()

	_, found := ti.tracked[fileOrDirPath]
	if found {
		return
	}
	fileInfo, err := os.Stat(fileOrDirPath)
	if err != nil {
		if os.IsNotExist(err) {
			err = errors.Wrapf(err, "path %q cannot be tracked because it does not exist", fileOrDirPath)
		} else {
			err = errors.Wrapf(err, "failed to track %q for changes", fileOrDirPath)
		}
	}

	// Create entry.
	entry := &trackEntry{
		IsDir:          fileInfo.IsDir(),
		UpdatedModTime: fileInfo.ModTime(),
	}
	ti.tracked[fileOrDirPath] = entry

	// Add watcher.
	if ti.watcher == nil {
		ti.watcher, err = fsnotify.NewWatcher()
		if err != nil {
			err = errors.Wrapf(err, "failed to create a filesystem watcher, not able to track file %q", fileOrDirPath)
			return
		}
		go func() {
			klog.V(2).Infof("goexec.State.Track(): Starting to listen to watcher")
			defer klog.V(2).Infof("goexec.State.Track(): Stopped to listen to watcher")

			for {
				select {
				case event, ok := <-ti.watcher.Events:
					if !ok {
						return
					}
					if event.Op != fsnotify.Write && event.Op != fsnotify.Remove {
						// Not interested.
						continue
					}
					if !isGoRelated(event.Name) {
						// Not interested.
						continue
					}
					ti.mu.Lock()
					klog.V(2).Infof("goexec.Track: updates to %q", event.Name)
					ti.updated.Insert(event.Name)
					ti.mu.Unlock()
				case err, ok := <-ti.watcher.Errors:
					klog.V(2).Infof("goexec.Track: async error received %+v", err)
					if !ok {
						return
					}
				}
			}
		}()
	}
	err = ti.watcher.Add(fileOrDirPath)
	if err != nil {
		err = errors.Wrapf(err, "Failed to watch tracked file/directory %q", fileOrDirPath)
		return
	}

	if entry.IsDir {
		err = filepath.WalkDir(fileOrDirPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return errors.Wrapf(err, "failed to track file under tracked directory %q", fileOrDirPath)
			}
			if d.IsDir() || !isGoRelated(path) {
				// Directories or files we don't care about.
				return nil
			}
			if !ti.updated.Has(path) {
				ti.updated.Insert(path)
				klog.V(2).Infof("tracking %q: added file for update %q", fileOrDirPath, path)
			}
			return nil
		})
	} else {
		ti.updated.Insert(fileOrDirPath)
	}
	return
}

// Untrack removes file or dir from path of tracked files. If it ends with "...", it un-tracks
// anything that has fileOrDirPath as prefix. If you set `fileOrDirPath == "..."`, it will
// un-tracks everything.
func (s *State) Untrack(fileOrDirPath string) (err error) {
	s.trackingInfo.mu.Lock()
	defer s.trackingInfo.mu.Unlock()

	if !strings.HasSuffix(fileOrDirPath, "...") {
		return s.lockedUntrackEntry(fileOrDirPath)
	}

	prefix := fileOrDirPath[:len(fileOrDirPath)-3]
	var toUntrack []string
	for p := range s.trackingInfo.tracked {
		if strings.HasPrefix(p, prefix) {
			toUntrack = append(toUntrack, p)
		}
	}
	for _, p := range toUntrack {
		err = s.lockedUntrackEntry(p)
		if err != nil {
			return err
		}
	}
	return
}

func (s *State) lockedUntrackEntry(fileOrDirPath string) (err error) {
	ti := s.trackingInfo
	entry, found := ti.tracked[fileOrDirPath]
	if !found {
		err = errors.Errorf("file or directory %q is not tracked, cannot untrack", fileOrDirPath)
		return
	}
	_ = entry
	delete(ti.tracked, fileOrDirPath)
	err = ti.watcher.Remove(fileOrDirPath)
	if err != nil {
		klog.V(2).Infof("goexec.Untrack failed to close watcher: %+v", err)
		err = nil
	}
	if len(ti.tracked) == 0 {
		klog.V(2).Infof("goexec.Untrack: nothing else to track, closing watcher")
		err = ti.watcher.Close()
		if err != nil {
			klog.V(2).Infof("goexec.Untrack failed to close watcher: %+v", err)
			err = nil
		}
		ti.watcher = nil
	}
	return
}

func (s *State) ListTracked() []string {
	s.trackingInfo.mu.Lock()
	defer s.trackingInfo.mu.Unlock()
	return common.SortedKeys(s.trackingInfo.tracked)
}

// isGoRelated checks whether a file is Go related.
func isGoRelated(fileOrDirPath string) bool {
	base := path.Base(fileOrDirPath)
	switch base {
	case "go.mod", "go.sum", "go.work":
		return true
	default:
		if strings.HasSuffix(base, "_test.go") {
			return false
		}
		if strings.HasSuffix(base, ".go") {
			return true
		}
	}
	return false
}

// EnumerateUpdatedFiles calls fn for each file that has been updated since
// the last call. If `fn` returns an error, then the enumerations is interrupted and
// the error is returned.
func (s *State) EnumerateUpdatedFiles(fn func(filePath string) error) (err error) {
	s.trackingInfo.mu.Lock()
	defer s.trackingInfo.mu.Unlock()

	files := common.SortedKeys(s.trackingInfo.updated)
	for _, filePath := range files {
		s.trackingInfo.updated.Delete(filePath)
		err = fn(filePath)
		if err != nil {
			return
		}
	}
	return
}

// AutoTrack adds automatic tracked directories. It looks at go.mod for
// redirects to the local filesystem.
// TODO: add support for go.work as well.
func (s *State) AutoTrack() (err error) {
	ti := s.trackingInfo
	goModPath := path.Join(s.TempDir, "go.mod")
	fileInfo, err := os.Stat(goModPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No go.mod, we dont' auto-track anything.
			err = nil
			return
		}
		err = errors.Wrapf(err, "failed to check %q for auto-tracking of files", goModPath)
		return
	}
	if !fileInfo.ModTime().After(ti.goModModTime) {
		// No changes.
		return
	}

	ti.goModModTime = fileInfo.ModTime()
	klog.V(2).Infof("goexec.AutoTrack: re-parsing %q for changes at %s", goModPath, ti.goModModTime)
	contents, err := os.ReadFile(goModPath)
	if err != nil {
		err = errors.Wrapf(err, "failed to read %q for auto-tracking of files", goModPath)
		return
	}
	matches := regexpGoModReplace.FindAllSubmatch(contents, -1)
	for _, match := range matches {
		replaceTarget := string(match[1])
		if replaceTarget[0] != '/' {
			// We only auto-track if the target of the replace is a local directory.
			continue
		}
		_, found := ti.tracked[replaceTarget]
		if found {
			// already tracked.
			continue
		}
		klog.Infof("- go.mod new replace: %s", replaceTarget)
		err = s.Track(replaceTarget)

		// Because fsnotify doesn't support recursion in watching for changes in subdirectories,
		// we need to add each subdirectory under the one defined.
		visited := common.MakeSet[string]()
		var visitorFn func(entryPath string, info fs.DirEntry, err error) error
		visitorFn = func(entryPath string, info fs.DirEntry, err error) error {
			// Check visited paths to break infinite loops with symbolic links.
			if visited.Has(entryPath) {
				return nil
			}
			visited.Insert(entryPath)

			// Visit function for each file in the directory:
			if err != nil {
				return errors.Wrapf(err, "failed to auto-track file under directory %q", replaceTarget)
			}
			if info.Type() == os.ModeSymlink {
				// Recursively follow symbolic links.
				linkedPath, err := os.Readlink(entryPath)
				if err != nil {
					err = errors.Wrapf(err, "looking for tracked files, failed to resolve symlink %q", entryPath)
					return err
				}
				return filepath.WalkDir(linkedPath, visitorFn)
			}
			if !isGoRelated(entryPath) {
				return nil
			}

			// Only track directories that have go files. Notice repeated tracked directories
			// are quickly ignored.
			dir := path.Dir(entryPath)
			return s.Track(dir)
		}
		err = filepath.WalkDir(replaceTarget, visitorFn)
		if err != nil {
			klog.Errorf("Failed to auto-track subdirectories of %q: %+v", replaceTarget, err)
			err = nil
		}
	}
	return
}

var (
	// `(?m)` makes "^" and "$" match beginning and end of line.
	regexpGoModReplace = regexp.MustCompile(`(?m)^\s*replace\s+.*?=>\s+(.*)$`)
)
