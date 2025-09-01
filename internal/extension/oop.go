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
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
)

const defaultUnixSocket = ".grpc.sock"

type OOPExtension struct {
	name       string
	executable string
	socket     string
	cmd        *exec.Cmd
	server     *grpc.Server
	service    extpb.ExtensionServiceServer
	logger     logr.Logger
	sync       io.Writer
	serverMu   sync.Mutex
	monitorWg  sync.WaitGroup
}

func NewOOPExtension(name string, location string, service extpb.ExtensionServiceServer, logger logr.Logger, sync io.Writer) (OOPExtension, error) {
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
		socket:     fmt.Sprintf("/tmp/kuadrant/%s/%s", name, defaultUnixSocket),
		executable: executable,
		service:    service,
		logger:     logger.WithName(name),
		sync:       sync,
	}, err
}

func (p *OOPExtension) Name() string {
	return p.name
}

func (p *OOPExtension) Start() error {
	p.logger.Info("starting...")

	if err := p.startServer(); err != nil {
		return err
	}

	cmd := exec.Command(p.executable, p.socket) // #nosec G204

	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.logger.Error(err, "failed to open stderr pipe")
		return err
	}

	p.monitorWg.Add(1)
	monitorReady := make(chan struct{})
	go p.monitorStderr(stderr, monitorReady)
	<-monitorReady

	if err = cmd.Start(); err != nil {
		if e := p.stopServer(); e != nil {
			p.logger.Error(e, "failed starting process, then stopping gRPC server failed")
		}
		return err
	}
	p.logger.Info("started")

	go func() {
		if e := cmd.Wait(); e != nil {
			p.logger.Error(e, fmt.Sprintf("Extension %q finished with an error", p.name))
		}
		// wait for stderr
		p.monitorWg.Wait()
	}()

	// only set this, if we successfully started it all
	p.cmd = cmd
	return nil
}

func (p *OOPExtension) IsAlive() bool {
	return p.cmd != nil && p.cmd.Process.Signal(syscall.Signal(0)) == nil
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

		if e := p.stopServer(); e != nil {
			if err == nil {
				err = e
			} else {
				p.logger.Error(e, "stopping gRPC server failed, while shutting down the process also failed")
			}
		}
		p.logger.Info("stopped")
		p.cmd = nil
	} else {
		p.logger.Info("nothing to stop")
	}

	return err
}

func (p *OOPExtension) startServer() error {
	p.serverMu.Lock()
	defer p.serverMu.Unlock()

	if p.server == nil {
		if err := os.MkdirAll(filepath.Dir(p.socket), 0755); err != nil {
			return fmt.Errorf("failed to create socket directory: %w", err)
		}

		ln, err := net.Listen("unix", p.socket)
		if err != nil {
			return err
		}

		server := grpc.NewServer()
		extpb.RegisterExtensionServiceServer(server, p.service)
		p.server = server

		go func() {
			if err := server.Serve(ln); err != nil {
				// FIXME: Make this fail synchronously somehow
				p.logger.Error(err, "failed to start server")
			}
		}()
	}
	return nil
}

func (p *OOPExtension) stopServer() error {
	p.serverMu.Lock()
	server := p.server
	p.server = nil
	p.serverMu.Unlock()

	if server != nil {
		server.Stop()
		if _, err := os.Stat(p.socket); err == nil {
			return os.Remove(p.socket)
		}
	}
	return nil
}

func (p *OOPExtension) monitorStderr(stderr io.ReadCloser, monitorReady chan struct{}) {
	defer p.monitorWg.Done()
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
