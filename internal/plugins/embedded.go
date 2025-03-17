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

package plugins

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const defaultUnixSocket = ".grpc.sock"

type EmbeddedPlugin struct {
	name       string
	executable string
	socket     string
	cmd        *exec.Cmd
	server     *grpc.Server
	logger     logr.Logger
}

func NewEmbeddedPlugin(name string, location string, logger logr.Logger) (EmbeddedPlugin, error) {
	var err error

	executable := fmt.Sprintf("%s/%s/%s", location, name, name)
	if stat, err := os.Stat(executable); err == nil {
		if stat.IsDir() || stat.Mode()&0111 == 0 {
			err = fmt.Errorf("%s: Not an executable", executable)
		}
	}

	return EmbeddedPlugin{
		name:       name,
		socket:     fmt.Sprintf("%s/%s/%s", location, name, defaultUnixSocket),
		executable: executable,
		logger:     logger,
	}, err
}

func (p *EmbeddedPlugin) Name() string {
	return p.name
}

func (p *EmbeddedPlugin) Start() error {
	p.logger.Info("Plugin `%s` starting...", p.name)

	if err := p.startServer(); err != nil {
		return err
	}

	cmd := exec.Command(p.executable, p.socket)
	if err := cmd.Start(); err != nil {
		p.stopServer()
		return err
	}
	p.logger.Info("Plugin `%s` started", p.name)

	// only set this, if we successfully started it all
	p.cmd = cmd
	return nil
}

func (p *EmbeddedPlugin) IsAlive() bool {
	return p.cmd != nil && p.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (p *EmbeddedPlugin) Stop() error {
	p.logger.Info("Plugin `%s` stopping...", p.name)
	var err error

	// Did we ever successfully started?
	if p.cmd != nil {
		if err = p.cmd.Process.Signal(syscall.SIGTERM); err == nil {
			timer := time.AfterFunc(2*time.Second, func() {
				p.cmd.Process.Kill() // we know this can fail, as this is racy. All that really matters is the `Wait()` below
			})

			if e := p.cmd.Wait(); e != nil {
				status := p.cmd.ProcessState.Sys().(syscall.WaitStatus)
				if !status.Signaled() || status.Signal() != syscall.SIGTERM {
					err = e
				}
			}

			timer.Stop()
		}

		if e := p.stopServer(); e != nil {
			if err == nil {
				err = e
			} else {
				p.logger.Error(e, "Plugin `%s` stopping gRPC server failed, while shutting down the plugin also failed", p.name)
			}
		}
		p.logger.Info("Plugin `%s` stopped", p.name)
		p.cmd = nil
	} else {
		p.logger.Info("Plugin `%s` nothing to stop", p.name)
	}

	return err
}

func (p *EmbeddedPlugin) startServer() error {
	if p.server == nil {
		ln, err := net.Listen("unix", p.socket)
		if err != nil {
			return err
		}

		p.server = grpc.NewServer()
		grpc_health_v1.RegisterHealthServer(p.server, health.NewServer())
		reflection.Register(p.server)

		go func() {
			p.server.Serve(ln)
		}()
	}
	return nil
}

func (p *EmbeddedPlugin) stopServer() error {
	if p.server != nil {
		p.server.Stop()
		p.server = nil
		if _, err := os.Stat(p.socket); err == nil {
			return os.Remove(p.socket)
		}
	}
	return nil
}
