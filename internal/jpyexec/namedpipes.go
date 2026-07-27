//go:build !windows

package jpyexec

import (
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"os"
	"sync"
	"syscall"
)

func (exec *Executor) handleNamedPipes() (err error) {
	exec.PipeWriterFifo = make(chan *protocol.CommValue, PipeWriterFifoBufferSize)

	exec.namedPipeReaderPath, err = exec.createTmpFifo()
	if err != nil {
		return errors.Wrapf(err, "creating named pipe used to read from program %s", exec.cmd)
	}
	exec.namedPipeWriterPath, err = exec.createTmpFifo()
	if err != nil {
		return errors.Wrapf(err, "creating named pipe used to write to program %s", exec.cmd)
	}
	exec.cmd.Env = append(exec.cmd.Environ(),
		protocol.GONB_PIPE_ENV+"="+exec.namedPipeReaderPath,
		protocol.GONB_PIPE_BACK_ENV+"="+exec.namedPipeWriterPath)

	exec.openPipeReader()
	exec.openPipeWriter()
	return
}

func (exec *Executor) createTmpFifo() (string, error) {
	f, err := os.CreateTemp(exec.dir, "gonb_pipe_")
	if err != nil {
		return "", err
	}
	pipePath := f.Name()
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Remove(pipePath); err != nil {
		return "", err
	}

	if err = syscall.Mkfifo(pipePath, 0600); err != nil {
		return "", errors.Wrapf(err, "failed to create pipe (Mkfifo) for %q", pipePath)
	}
	return pipePath, nil
}

func (exec *Executor) openPipeReader() {
	var muFifo sync.Mutex
	fifoOpenedForReading := false

	go func() {
		<-exec.doneChan
		muFifo.Lock()
		if !fifoOpenedForReading {
			w, err := os.OpenFile(exec.namedPipeReaderPath, os.O_WRONLY, 0600)
			if err == nil {
				_ = w.Close()
			}
		}
		muFifo.Unlock()
		_ = os.Remove(exec.namedPipeReaderPath)
	}()

	go func() {
		klog.V(2).Infof("Opening named pipeReader in %q", exec.namedPipeReaderPath)
		if exec.isDone {
			return
		}
		var err error
		exec.pipeReader, err = os.Open(exec.namedPipeReaderPath)
		if err != nil {
			klog.Warningf("Failed to open pipe (Mkfifo) %q for reading: %+v", exec.namedPipeReaderPath, err)
			return
		}
		klog.V(2).Infof("Opened named pipeReader in %q", exec.namedPipeReaderPath)
		muFifo.Lock()
		fifoOpenedForReading = true
		defer muFifo.Unlock()

		go exec.pollNamedPipeReader()

		<-exec.doneChan
		_ = exec.pipeReader.Close()
		_ = os.Remove(exec.namedPipeReaderPath)
	}()
}

func (exec *Executor) openPipeWriter() {
	var muFifo sync.Mutex
	fifoOpened := false

	go func() {
		<-exec.doneChan
		muFifo.Lock()
		if !fifoOpened {
			r, err := os.OpenFile(exec.namedPipeWriterPath, os.O_RDONLY, 0600)
			if err == nil {
				_ = r.Close()
			}
		}
		muFifo.Unlock()
		_ = os.Remove(exec.namedPipeWriterPath)
	}()

	go func() {
		klog.V(2).Infof("Opening named pipeWriter in %q", exec.namedPipeWriterPath)
		if exec.isDone {
			klog.Warningf("Opening of NamedPipeWriter in %q failed, since program already stopped/crashed", exec.namedPipeWriterPath)
			return
		}
		f, err := os.OpenFile(exec.namedPipeWriterPath, os.O_WRONLY, 0600)
		if err != nil {
			klog.Warningf("Failed to open pipe (Mkfifo) %q for writing: %+v", exec.namedPipeWriterPath, err)
			return
		}
		klog.V(2).Infof("Opened named pipeWriter in %q", exec.namedPipeWriterPath)
		muFifo.Lock()
		exec.pipeWriter = f
		fifoOpened = true
		defer muFifo.Unlock()

		go exec.pollPipeWriterFifo()

		<-exec.doneChan
		close(exec.PipeWriterFifo)
		_ = exec.pipeWriter.Close()
		_ = os.Remove(exec.namedPipeWriterPath)
	}()
}
