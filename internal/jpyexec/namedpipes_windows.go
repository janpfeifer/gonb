//go:build windows

package jpyexec

import (
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"k8s.io/klog/v2"
	"os"
	"sync"
)

func (exec *Executor) handleNamedPipes() (err error) {
	exec.PipeWriterFifo = make(chan *protocol.CommValue, PipeWriterFifoBufferSize)

	exec.namedPipeReaderPath, exec.namedPipeReaderHandle, err = createWindowsPipe(exec.dir)
	if err != nil {
		return errors.Wrapf(err, "creating Windows named pipe to read from program %s", exec.cmd)
	}
	exec.namedPipeWriterPath, exec.namedPipeWriterHandle, err = createWindowsPipe(exec.dir)
	if err != nil {
		cleanupHandle(exec.namedPipeReaderHandle)
		return errors.Wrapf(err, "creating Windows named pipe to write to program %s", exec.cmd)
	}

	exec.cmd.Env = append(exec.cmd.Environ(),
		protocol.GONB_PIPE_ENV+"="+exec.namedPipeReaderPath,
		protocol.GONB_PIPE_BACK_ENV+"="+exec.namedPipeWriterPath)

	exec.openPipeReader()
	exec.openPipeWriter()
	return
}

func createWindowsPipe(dir string) (pipePath string, rawHandle uintptr, err error) {
	pipePath = `\\.\pipe\gonb_` + common.UniqueId()
	pathp, err := windows.UTF16PtrFromString(pipePath)
	if err != nil {
		return "", 0, err
	}

	handle, err := windows.CreateNamedPipe(
		pathp,
		windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		windows.PIPE_UNLIMITED_INSTANCES,
		65536, 65536,
		0, nil)
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to create Windows named pipe %q", pipePath)
	}
	return pipePath, uintptr(handle), nil
}

func cleanupHandle(rawHandle uintptr) {
	if rawHandle != 0 {
		_ = windows.CloseHandle(windows.Handle(rawHandle))
	}
}

func (exec *Executor) openPipeReader() {
	var muFifo sync.Mutex
	fifoOpenedForReading := false

	go func() {
		<-exec.doneChan
		muFifo.Lock()
		if !fifoOpenedForReading {
			cleanupHandle(exec.namedPipeReaderHandle)
		}
		muFifo.Unlock()
	}()

	go func() {
		klog.V(2).Infof("Connecting Windows named pipeReader in %q", exec.namedPipeReaderPath)
		if exec.isDone {
			return
		}
		handle := windows.Handle(exec.namedPipeReaderHandle)
		err := windows.ConnectNamedPipe(handle, nil)
		if err != nil && err != windows.ERROR_PIPE_CONNECTED {
			klog.Warningf("Failed to connect Windows named pipe %q for reading: %+v", exec.namedPipeReaderPath, err)
			return
		}
		klog.V(2).Infof("Connected Windows named pipeReader in %q", exec.namedPipeReaderPath)
		exec.pipeReader = os.NewFile(exec.namedPipeReaderHandle, exec.namedPipeReaderPath)
		muFifo.Lock()
		fifoOpenedForReading = true
		defer muFifo.Unlock()

		go exec.pollNamedPipeReader()

		<-exec.doneChan
		if exec.pipeReader != nil {
			_ = exec.pipeReader.Close()
		}
	}()
}

func (exec *Executor) openPipeWriter() {
	var muFifo sync.Mutex
	fifoOpened := false

	go func() {
		<-exec.doneChan
		muFifo.Lock()
		if !fifoOpened {
			cleanupHandle(exec.namedPipeWriterHandle)
		}
		muFifo.Unlock()
	}()

	go func() {
		klog.V(2).Infof("Connecting Windows named pipeWriter in %q", exec.namedPipeWriterPath)
		if exec.isDone {
			klog.Warningf("Connecting Windows NamedPipeWriter in %q failed, since program already stopped/crashed", exec.namedPipeWriterPath)
			return
		}
		handle := windows.Handle(exec.namedPipeWriterHandle)
		err := windows.ConnectNamedPipe(handle, nil)
		if err != nil && err != windows.ERROR_PIPE_CONNECTED {
			klog.Warningf("Failed to connect Windows named pipe %q for writing: %+v", exec.namedPipeWriterPath, err)
			return
		}
		klog.V(2).Infof("Connected Windows named pipeWriter in %q", exec.namedPipeWriterPath)
		f := os.NewFile(exec.namedPipeWriterHandle, exec.namedPipeWriterPath)
		muFifo.Lock()
		exec.pipeWriter = f
		fifoOpened = true
		defer muFifo.Unlock()

		go exec.pollPipeWriterFifo()

		<-exec.doneChan
		close(exec.PipeWriterFifo)
		if exec.pipeWriter != nil {
			_ = exec.pipeWriter.Close()
		}
	}()
}
