/*
Copyright 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/go-logr/logr"
)

type OOPExtension struct {
	name         string
	executable   string
	port         int
	credential   []byte
	cmd          *exec.Cmd
	logger       logr.Logger
	sync         io.Writer
	monitorWg    sync.WaitGroup
	completionWg sync.WaitGroup
}

func NewOOPExtension(name string, location string, credential []byte, port int, logger logr.Logger, sync io.Writer) (OOPExtension, error) {
	var err error
	var stat os.FileInfo

	executable := fmt.Sprintf("%s/%s/%s", location, name, name)
	if stat, err = os.Stat(executable); err == nil {
		if stat.IsDir() || stat.Mode()&0111 == 0 {
			err = fmt.Errorf("%s: Not an executable", executable)
		}
	}

	return OOPExtension{
		name:       name,
		port:       port,
		credential: credential,
		executable: executable,
		logger:     logger.WithName(name),
		sync:       sync,
	}, err
}

func (p *OOPExtension) Name() string {
	return p.name
}

func (p *OOPExtension) Start() error {
	p.logger.Info("starting...")

	cmd := exec.Command(p.executable) // #nosec G204
	cmd.Env = append(os.Environ(),
		"KUADRANT_EXTENSION_CREDENTIAL="+string(p.credential),
		fmt.Sprintf("KUADRANT_EXTENSION_ADDRESS=localhost:%d", p.port),
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.logger.Error(err, "failed to open stderr pipe")
		return err
	}

	monitorReady := make(chan struct{})
	p.monitorWg.Go(func() {
		p.monitorStderr(stderr, monitorReady)
	})
	<-monitorReady

	if err = cmd.Start(); err != nil {
		return err
	}
	p.logger.Info("started")

	p.completionWg.Go(func() {
		// We must wait for stderr to be fully read before calling cmd.Wait()
		p.monitorWg.Wait()

		if e := cmd.Wait(); e != nil {
			p.logger.Error(e, fmt.Sprintf("Extension %q finished with an error", p.name))
		}
	})

	// only set this, if we successfully started it all
	p.cmd = cmd
	return nil
}

func (p *OOPExtension) IsAlive() bool {
	return p.cmd != nil && p.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (p *OOPExtension) WaitForCompletion() {
	p.completionWg.Wait()
}

func (p *OOPExtension) Stop() error {
	p.logger.Info("stopping...")
	var err error

	// Did we ever successfully started?
	if p.cmd != nil {
		if err = p.cmd.Process.Signal(syscall.SIGTERM); err == nil {
			timer := time.AfterFunc(2*time.Second, func() {
				_ = p.cmd.Process.Kill() // we know this can fail, as this is racy. All that really matters is the `Wait()` below
			})

			timer.Stop()
		}

		// let stderr monitoring finish
		p.monitorWg.Wait()

		p.logger.Info("stopped")
		p.cmd = nil
	} else {
		p.logger.Info("nothing to stop")
	}

	return err
}

func (p *OOPExtension) monitorStderr(stderr io.ReadCloser, monitorReady chan struct{}) {
	defer stderr.Close()

	scanner := bufio.NewScanner(stderr)

	// signal readiness
	close(monitorReady)

	for scanner.Scan() {
		// TODO (didierofrivia): Check output of scanner.Bytes() to see if it's sink compatible, otherwise log
		if _, err := p.sync.Write(append(scanner.Bytes(), []byte("\n")...)); err != nil {
			p.logger.Error(err, "failed to write to logger")
		}
	}

	// check if the scanner stopped due to error
	if err := scanner.Err(); err != nil {
		p.logger.Error(err, "failed to read stderr")
	}
}
