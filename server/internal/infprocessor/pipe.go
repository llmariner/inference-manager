package infprocessor

import "io"

func newPipeReadWriteCloser() pipeReadWriteCloser {
	pr, pw := io.Pipe()
	return pipeReadWriteCloser{
		PipeReader: pr,
		PipeWriter: pw,
	}
}

type pipeReadWriteCloser struct {
	*io.PipeReader
	*io.PipeWriter
}

// Close closes the pipe.
func (c pipeReadWriteCloser) Close() error {
	if err := c.PipeReader.Close(); err != nil {
		return err
	}
	if err := c.PipeWriter.Close(); err != nil {
		return err
	}
	return nil
}

// closeWrite closes the write pipe.
func (c pipeReadWriteCloser) closeWrite() error {
	if err := c.PipeWriter.Close(); err != nil {
		return err
	}
	return nil
}
