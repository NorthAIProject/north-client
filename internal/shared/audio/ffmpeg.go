package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrNotInstalled is what a deployment without ffmpeg gets, once, at boot.
//
// Its own error because it has a specific cause and a specific fix, and because
// it never resolves on its own: the caller should switch a feature off and say
// so, not retry.
var ErrNotInstalled = errors.New("audio: ffmpeg is not installed")

const (
	// transcodeTimeout bounds one conversion. A four-minute recording decodes
	// in well under a second; thirty is the difference between slow and stuck.
	transcodeTimeout = 30 * time.Second

	// maxOutputBytes bounds what ffmpeg may hand back.
	//
	// Not the same number as the caller's ceiling, and deliberately generous:
	// this is the wall that stops a crafted file from filling memory, while the
	// caller's own limit is what decides whether a recording is reasonable.
	maxOutputBytes = 64 << 20

	// maxStderrBytes bounds the diagnostic. Enough to name the problem, not
	// enough to put a codec's life story in an error.
	maxStderrBytes = 4 << 10

	// maxConcurrent caps how many conversions run at once.
	//
	// This is the first thing in North that forks a process, and it does it in
	// the web pod's request path. Ten people sending voice notes in the same
	// second is a burst; ten ffmpeg processes competing for a pod's memory is
	// an outage. Queueing is the right answer because the work is short.
	maxConcurrent = 4
)

// FFmpeg converts recordings by shelling out.
//
// A binary rather than a Go codec because there is no pure-Go Opus decoder
// worth depending on — the options are cgo bindings to libopus or this, and
// this keeps the decoder out of the address space. What it costs is a package
// in the image and a class of failure Go code does not have, which is why every
// invocation below is bounded in four different ways.
type FFmpeg struct {
	bin string

	sem chan struct{}

	// opusOnce probes the encoder list lazily and exactly once. Asking the
	// binary what it can do beats assuming what a distribution shipped.
	opusOnce sync.Once
	opusOK   bool
}

// NewFFmpeg finds the binary, or says why it could not.
//
// bin may be empty to look on PATH. Resolved once, at boot, so a missing ffmpeg
// is a line in the log rather than something every recording rediscovers.
func NewFFmpeg(bin string) (*FFmpeg, error) {
	if bin == "" {
		bin = "ffmpeg"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotInstalled, bin)
	}
	return &FFmpeg{bin: path, sem: make(chan struct{}, maxConcurrent)}, nil
}

// ToWAV decodes any container ffmpeg can read into 16 kHz mono WAV.
//
// The output format is not a preference. It is what the OpenAI dialect names,
// at the rate speech recognition actually uses, and it is byte-for-byte the
// shape web/assets/js/shared/capture-recorder.js already produces in the
// browser — so the two surfaces hand a model the same thing.
func (f *FFmpeg) ToWAV(ctx context.Context, in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("audio: there is nothing to convert")
	}
	// -vn drops any video track, because a container holding both would
	// otherwise fail rather than yield its audio.
	//
	// No -f is passed for the input: the declared type is a claim and ffmpeg's
	// demuxer is what actually knows. Guessing here would refuse files that are
	// fine and mislabel files that are not.
	return f.run(ctx, in,
		"-i", "pipe:0",
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		"-f", "wav",
		"pipe:1",
	)
}

// CanEncodeOpus reports whether this binary can produce Opus.
//
// Asked rather than assumed, because a distribution's ffmpeg may be built
// without it, and the failure that causes is a feature that is quietly off. One
// probe at boot turns that into something a log line can say.
func (f *FFmpeg) CanEncodeOpus() bool {
	f.opusOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, f.bin, "-hide_banner", "-loglevel", "error", "-encoders")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return
		}
		f.opusOK = strings.Contains(out.String(), "libopus") || strings.Contains(out.String(), " opus ")
	})
	return f.opusOK
}

// run is the only place this package starts a process, and every rule that
// keeps that safe lives here rather than being repeated per conversion.
func (f *FFmpeg) run(ctx context.Context, in []byte, args ...string) ([]byte, error) {
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, transcodeTimeout)
	defer cancel()

	// A fixed argument slice, never a shell and never a format string. Nothing
	// a person sends can become an argument: the recording travels on stdin and
	// comes back on stdout, so there is no filename, no path, and nothing to
	// clean up afterwards.
	argv := append([]string{"-nostdin", "-hide_banner", "-loglevel", "error"}, args...)
	cmd := exec.CommandContext(ctx, f.bin, argv...)
	cmd.Stdin = bytes.NewReader(in)

	// WaitDelay so a process that ignores the kill cannot pin this goroutine
	// holding a slot in the semaphore.
	cmd.WaitDelay = 5 * time.Second

	var out, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &out, limit: maxOutputBytes}
	cmd.Stderr = &limitedWriter{buf: &stderr, limit: maxStderrBytes}

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("audio: conversion stopped: %w", ctxErr)
		}
		return nil, fmt.Errorf("audio: convert: %s", strings.TrimSpace(stderr.String()))
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("audio: the conversion produced nothing")
	}
	return out.Bytes(), nil
}

// limitedWriter stops a process writing more than it was asked for.
//
// It reports an error rather than truncating silently, because half a recording
// is a recording that says something the person did not.
type limitedWriter struct {
	buf     *bytes.Buffer
	limit   int
	written int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.written+len(p) > w.limit {
		return 0, fmt.Errorf("audio: the conversion produced more than %d bytes", w.limit)
	}
	w.written += len(p)
	return w.buf.Write(p)
}
