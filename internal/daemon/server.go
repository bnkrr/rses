package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"
	"time"

	"github.com/bnkrr/rses/internal/paths"
)

type Server struct {
	locksMu      sync.Mutex
	sessionLocks map[string]*sync.Mutex
	shutdown     chan struct{}
	once         sync.Once
}

var processStartedAt = time.Now()

func RunForeground() error {
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.RuntimeDir(), 0o755); err != nil {
		return err
	}

	socket := paths.SocketPath()
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer os.Remove(socket)

	server := &Server{shutdown: make(chan struct{})}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		select {
		case <-server.shutdown:
			_ = conn.Close()
			return nil
		default:
		}
		go server.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResp(conn, Response{OK: false, Error: err.Error()})
		return
	}

	var resp Response
	switch req.Action {
	case "meta":
		resp = Response{OK: true, Meta: ptr(daemonMeta())}
	case "shutdown":
		resp = Response{OK: true}
		go s.requestShutdown()
	case "start":
		s.stream(conn, req, s.start)
		return
	case "stop":
		s.stream(conn, req, s.stop)
		return
	case "status":
		s.stream(conn, req, s.status)
		return
	case "watch-snapshot":
		resp = s.watchSnapshot(req)
	case "logs":
		resp = s.logs(req)
	default:
		resp = Response{OK: false, Error: "unknown daemon action: " + req.Action}
	}
	writeResp(conn, resp)
}

func (s *Server) requestShutdown() {
	s.once.Do(func() {
		close(s.shutdown)
		conn, err := net.DialTimeout("unix", paths.SocketPath(), 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
	})
}

func writeResp(conn net.Conn, resp Response) {
	_ = json.NewEncoder(conn).Encode(resp)
}

type eventEmitter func(OperationEvent) error
type streamHandler func(context.Context, Request, eventEmitter) operationSummary

func (s *Server) stream(conn net.Conn, req Request, run streamHandler) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	enc := json.NewEncoder(conn)
	var writeMu sync.Mutex
	var writeErr error
	emit := func(event OperationEvent) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if writeErr != nil {
			return writeErr
		}
		if event.Action == "" {
			event.Action = req.Action
		}
		if err := enc.Encode(event); err != nil {
			writeErr = err
			cancel()
			return err
		}
		return nil
	}
	summary := run(ctx, req, emit)
	_ = emit(OperationEvent{
		Type:     "done",
		Action:   req.Action,
		Results:  summary.Results,
		Statuses: summary.Statuses,
	})
}

func (s *Server) lockSession(id operationID) func() {
	key := id.String()
	s.locksMu.Lock()
	if s.sessionLocks == nil {
		s.sessionLocks = map[string]*sync.Mutex{}
	}
	lock := s.sessionLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.sessionLocks[key] = lock
	}
	s.locksMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func ptr[T any](value T) *T {
	return &value
}
