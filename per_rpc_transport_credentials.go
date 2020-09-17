// Copyright (c) 2020 Cisco and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpcfd

import (
	context "context"
	"os"

	"github.com/edwarnicke/serialize"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type wrapPerRPCCredentials struct {
	credentials.PerRPCCredentials
	FDTransceiver
	transceiverFuncs []func(FDTransceiver)
	executor         serialize.Executor
}

func (w *wrapPerRPCCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	<-w.executor.AsyncExec(func() {
		if w.FDTransceiver != nil {
			return
		}
		if trans, ok := FromContext(ctx); ok {
			w.FDTransceiver = trans
			for _, f := range w.transceiverFuncs {
				f(trans)
			}
			w.transceiverFuncs = nil
		}
	})
	if w.PerRPCCredentials != nil {
		return w.PerRPCCredentials.GetRequestMetadata(ctx, uri...)
	}
	return map[string]string{}, nil
}

func (w *wrapPerRPCCredentials) RequireTransportSecurity() bool {
	if w.PerRPCCredentials != nil {
		return w.PerRPCCredentials.RequireTransportSecurity()
	}
	return false
}

func (w *wrapPerRPCCredentials) SendFD(fd uintptr) <-chan error {
	out := make(chan error, 1)
	var transceiver FDTransceiver
	w.executor.AsyncExec(func() {
		if w.FDTransceiver != nil {
			transceiver = w.FDTransceiver
			return
		}
		w.transceiverFuncs = append(w.transceiverFuncs, func(transceiver FDTransceiver) {
			go joinErrChs(transceiver.SendFD(fd), out)
		})
	})
	if transceiver != nil {
		return transceiver.SendFD(fd)
	}
	return out
}

func (w *wrapPerRPCCredentials) SendFile(file SyscallConn) <-chan error {
	out := make(chan error, 1)
	var transceiver FDTransceiver
	w.executor.AsyncExec(func() {
		if w.FDTransceiver != nil {
			transceiver = w.FDTransceiver
			return
		}
		w.transceiverFuncs = append(w.transceiverFuncs, func(transceiver FDTransceiver) {
			go joinErrChs(transceiver.SendFile(file), out)
		})
	})
	if transceiver != nil {
		return transceiver.SendFile(file)
	}
	return out
}

func (w *wrapPerRPCCredentials) RecvFD(dev, inode uint64) <-chan uintptr {
	out := make(chan uintptr, 1)
	var transceiver FDTransceiver
	w.executor.AsyncExec(func() {
		if w.FDTransceiver != nil {
			transceiver = w.FDTransceiver
			return
		}
		w.transceiverFuncs = append(w.transceiverFuncs, func(transceiver FDTransceiver) {
			go joinFDChs(transceiver.RecvFD(dev, inode), out)
		})
	})
	if transceiver != nil {
		return transceiver.RecvFD(dev, inode)
	}
	return out
}

func (w *wrapPerRPCCredentials) RecvFile(dev, ino uint64) <-chan *os.File {
	out := make(chan *os.File, 1)
	var transceiver FDTransceiver
	w.executor.AsyncExec(func() {
		if w.FDTransceiver != nil {
			transceiver = w.FDTransceiver
			return
		}
		w.transceiverFuncs = append(w.transceiverFuncs, func(transceiver FDTransceiver) {
			go joinFileChs(transceiver.RecvFile(dev, ino), out)
		})
	})
	if transceiver != nil {
		return transceiver.RecvFile(dev, ino)
	}
	return out
}

func (w *wrapPerRPCCredentials) RecvFileByURL(urlStr string) (<-chan *os.File, error) {
	dev, ino, err := URLStringToDevIno(urlStr)
	if err != nil {
		return nil, err
	}
	out := make(chan *os.File, 1)
	var transceiver FDTransceiver
	w.executor.AsyncExec(func() {
		if w.FDTransceiver != nil {
			transceiver = w.FDTransceiver
			return
		}
		w.transceiverFuncs = append(w.transceiverFuncs, func(transceiver FDTransceiver) {
			go joinFileChs(transceiver.RecvFile(dev, ino), out)
		})
	})
	if transceiver != nil {
		return transceiver.RecvFileByURL(urlStr)
	}
	return out, nil
}

func (w *wrapPerRPCCredentials) RecvFDByURL(urlStr string) (<-chan uintptr, error) {
	dev, ino, err := URLStringToDevIno(urlStr)
	if err != nil {
		return nil, err
	}
	out := make(chan uintptr, 1)
	var transceiver FDTransceiver
	w.executor.AsyncExec(func() {
		if w.FDTransceiver != nil {
			transceiver = w.FDTransceiver
			return
		}
		w.transceiverFuncs = append(w.transceiverFuncs, func(transceiver FDTransceiver) {
			go joinFDChs(transceiver.RecvFD(dev, ino), out)
		})
	})
	if transceiver != nil {
		return transceiver.RecvFDByURL(urlStr)
	}
	return out, nil
}

func joinErrChs(in <-chan error, out chan<- error) {
	for err := range in {
		out <- err
	}
	close(out)
}

func joinFileChs(in <-chan *os.File, out chan<- *os.File) {
	for file := range in {
		out <- file
	}
	close(out)
}

func joinFDChs(in <-chan uintptr, out chan<- uintptr) {
	for fd := range in {
		out <- fd
	}
	close(out)
}

// PerRPCCredentials - per rpc credentials that will, in addition to applying cred, invoke sendFunc
// Note: Must be used in concert with grpcfd.TransportCredentials
func PerRPCCredentials(cred credentials.PerRPCCredentials) credentials.PerRPCCredentials {
	return &wrapPerRPCCredentials{
		PerRPCCredentials: cred,
	}
}

// PerRPCCredentialsFromCallOptions - extract credentials.PerRPCCredentials from a list of grpc.CallOptions
func PerRPCCredentialsFromCallOptions(opts ...grpc.CallOption) credentials.PerRPCCredentials {
	for i := len(opts) - 1; i >= 0; i-- {
		if prcp, ok := opts[i].(grpc.PerRPCCredsCallOption); ok {
			return prcp.Creds
		}
	}
	return nil
}

// FromPerRPCCredentials - return grpcfd.FDTransceiver from credentials.PerRPCCredentials
//                         ok is true of successful, false otherwise
func FromPerRPCCredentials(rpcCredentials credentials.PerRPCCredentials) (transceiver FDTransceiver, ok bool) {
	if transceiver, ok = rpcCredentials.(FDTransceiver); ok {
		return transceiver, true
	}
	return nil, false
}