package agent

import (
	"context"
	"errors"
	"io"
	"log"
	"os/exec"
	"runtime"
	"sync"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// outputChunk is the byte size of each streamed read; small enough to feel live,
// large enough to avoid frame spam.
const outputChunk = 4096

// runCommand executes one hub-dispatched command via the platform shell,
// streaming stdout and stderr back as command_output frames, then sends a
// command_exit frame with the real exit code.
func runCommand(ctx context.Context, rc proto.RunCommandPayload, outbound chan<- []byte) {
	cmd := shellCommand(ctx, rc.Command)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendExit(ctx, outbound, rc.CmdID, -1, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		sendExit(ctx, outbound, rc.CmdID, -1, err)
		return
	}

	if err := cmd.Start(); err != nil {
		sendExit(ctx, outbound, rc.CmdID, -1, err)
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); streamPipe(ctx, outbound, rc.CmdID, "stdout", stdout) }()
	go func() { defer wg.Done(); streamPipe(ctx, outbound, rc.CmdID, "stderr", stderr) }()
	wg.Wait()

	exitCode := 0
	var runErr error
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
			runErr = err
		}
	}
	sendExit(ctx, outbound, rc.CmdID, exitCode, runErr)
}

// shellCommand wires the command through the platform shell.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/c", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

// streamPipe reads a pipe in chunks and emits a command_output frame per chunk.
func streamPipe(ctx context.Context, outbound chan<- []byte, cmdID, stream string, r io.Reader) {
	buf := make([]byte, outputChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			frame, encErr := proto.Encode(proto.TypeCommandOutput, proto.CommandOutputPayload{
				CmdID:  cmdID,
				Stream: stream,
				Data:   string(buf[:n]),
			})
			if encErr != nil {
				log.Printf("agent: encode output: %v", encErr)
			} else {
				select {
				case outbound <- frame:
				case <-ctx.Done():
					return
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("agent: read %s for %s: %v", stream, cmdID, err)
			}
			return
		}
	}
}

// sendExit emits the terminal command_exit frame.
func sendExit(ctx context.Context, outbound chan<- []byte, cmdID string, code int, runErr error) {
	payload := proto.CommandExitPayload{CmdID: cmdID, ExitCode: code}
	if runErr != nil {
		payload.Error = runErr.Error()
	}
	frame, err := proto.Encode(proto.TypeCommandExit, payload)
	if err != nil {
		log.Printf("agent: encode exit: %v", err)
		return
	}
	select {
	case outbound <- frame:
	case <-ctx.Done():
	}
}
