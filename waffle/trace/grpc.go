package trace

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"gitlab.com/gorib/failure"

	record "github.com/specrig/specrig-go/trace"
)

// Inbound records every call the suite makes to the service's own server.
func Inbound(conn grpc.ClientConnInterface) grpc.ClientConnInterface { return inbound{conn} }

type inbound struct{ grpc.ClientConnInterface }

func (c inbound) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	began := record.Begin()
	err := c.ClientConnInterface.Invoke(ctx, method, args, reply, opts...)
	call(began, "in", method, args, reply, err)
	return err
}

// The generated NewXClient over it hands each call to client by method name.
func Outbound(client any) grpc.ClientConnInterface { return outbound{reflect.ValueOf(client)} }

type outbound struct{ target reflect.Value }

func (c outbound) Invoke(ctx context.Context, method string, args, reply any, _ ...grpc.CallOption) error {
	fn := c.target.MethodByName(method[strings.LastIndex(method, "/")+1:])
	if !fn.IsValid() {
		return status.Errorf(codes.Unimplemented, "trace: %s has no method for %s", c.target.Type(), method)
	}
	results := fn.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(args)})
	err, _ := results[1].Interface().(error)
	if err == nil && !results[0].IsNil() {
		proto.Merge(reply.(proto.Message), results[0].Interface().(proto.Message))
	}
	call(0, "out", method, args, reply, err)
	return err
}

func (c outbound) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
	fn := c.target.MethodByName(method[strings.LastIndex(method, "/")+1:])
	if !fn.IsValid() {
		return nil, status.Errorf(codes.Unimplemented, "trace: %s has no method for %s", c.target.Type(), method)
	}
	s := &fakeStream{ctx: ctx, desc: desc, fn: fn, recording: newRecording("out", method, desc)}
	if desc.ClientStreams {
		if err := s.open(reflect.ValueOf(ctx)); err != nil {
			return nil, err
		}
	}
	return s, nil
}

type fakeStream struct {
	*recording
	ctx     context.Context
	desc    *grpc.StreamDesc
	fn      reflect.Value
	request any
	stream  reflect.Value
}

func (s *fakeStream) open(args ...reflect.Value) error {
	results := s.fn.Call(args)
	if err, _ := results[1].Interface().(error); err != nil {
		s.end(err)
		return err
	}
	if results[0].IsNil() {
		err := status.Errorf(codes.Internal, "trace: %s returned no stream for %s", s.fn.Type(), s.method)
		s.end(err)
		return err
	}
	s.stream = results[0]
	return nil
}

func (s *fakeStream) SendMsg(m any) error {
	s.send(m)
	if !s.desc.ClientStreams {
		s.request = m
		return nil
	}
	return s.invoke("Send", reflect.ValueOf(m))
}

// The generated client sends a server stream's only request and closes the
// send side before handing the stream out, so that is when the fake is called.
func (s *fakeStream) CloseSend() error {
	switch {
	case !s.desc.ClientStreams:
		return s.open(reflect.ValueOf(s.ctx), reflect.ValueOf(s.request))
	case s.desc.ServerStreams:
		return s.invoke("CloseSend")
	default:
		return nil
	}
}

func (s *fakeStream) RecvMsg(m any) error {
	recv := "Recv"
	if !s.desc.ServerStreams {
		recv = "CloseAndRecv"
	}
	results := s.stream.MethodByName(recv).Call(nil)
	if err, _ := results[1].Interface().(error); err != nil {
		s.end(err)
		return err
	}
	if !results[0].IsNil() {
		proto.Merge(m.(proto.Message), results[0].Interface().(proto.Message))
	}
	s.receive(m)
	return nil
}

func (s *fakeStream) invoke(name string, args ...reflect.Value) error {
	err, _ := s.stream.MethodByName(name).Call(args)[0].Interface().(error)
	if err != nil {
		s.end(err)
	}
	return err
}

func (s *fakeStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *fakeStream) Trailer() metadata.MD         { return metadata.MD{} }
func (s *fakeStream) Context() context.Context     { return s.ctx }

func (c inbound) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	r := newRecording("in", method, desc)
	stream, err := c.ClientConnInterface.NewStream(ctx, desc, method, opts...)
	if err != nil {
		r.end(err)
		return nil, err
	}
	return inboundStream{ClientStream: stream, recording: r}, nil
}

type inboundStream struct {
	grpc.ClientStream
	*recording
}

func (s inboundStream) SendMsg(m any) error {
	err := s.ClientStream.SendMsg(m)
	if err == nil {
		s.send(m)
	}
	return err
}

func (s inboundStream) RecvMsg(m any) error {
	if err := s.ClientStream.RecvMsg(m); err != nil {
		s.end(err)
		return err
	}
	s.receive(m)
	return nil
}

type recording struct {
	began    int64
	dir      string
	method   string
	oneReply bool
	mu       sync.Mutex
	sent     []json.RawMessage
	received []json.RawMessage
	ended    bool
}

func newRecording(dir, method string, desc *grpc.StreamDesc) *recording {
	return &recording{began: record.Begin(), dir: dir, method: method, oneReply: !desc.ServerStreams}
}

func (r *recording) send(m any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, message(m))
}

func (r *recording) receive(m any) {
	r.mu.Lock()
	r.received = append(r.received, message(m))
	r.mu.Unlock()
	if r.oneReply {
		r.end(nil)
	}
}

func (r *recording) end(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended || !record.On() {
		return
	}
	r.ended = true
	out := record.Record{Seq: r.began, Dir: r.dir, Kind: "grpc", Name: r.method, Request: list(r.sent)}
	if err != nil && !errors.Is(err, io.EOF) {
		out.Error = refusal(err)
	} else {
		out.Response = list(r.received)
	}
	record.Write(out)
}

func list(messages []json.RawMessage) json.RawMessage {
	if messages == nil {
		messages = []json.RawMessage{}
	}
	body, _ := json.Marshal(messages)
	return body
}

func call(began int64, dir, method string, args, reply any, err error) {
	if !record.On() {
		return
	}
	r := record.Record{Seq: began, Dir: dir, Kind: "grpc", Name: method, Request: message(args)}
	if err != nil {
		r.Error = refusal(err)
	} else {
		r.Response = message(reply)
	}
	record.Write(r)
}

func refusal(err error) string {
	var parts []string
	st, isStatus := status.FromError(err)
	if isStatus {
		parts = append(parts, st.Code().String())
	}
	if fe, ok := failure.FromError(err); ok && fe.Type() != "" {
		return strings.Join(append(parts, fe.Type()), " ")
	}
	if isStatus {
		for _, d := range st.Details() {
			if info, ok := d.(*errdetails.ErrorInfo); ok && info.Domain == failure.Kind {
				return strings.Join(append(parts, strings.ToLower(info.Reason)), " ")
			}
		}
		return strings.Join(parts, " ")
	}
	return "failed"
}

func message(v any) json.RawMessage {
	if m, ok := v.(proto.Message); ok {
		if body, err := protojson.Marshal(m); err == nil {
			return body
		}
	}
	body, _ := json.Marshal(v)
	return body
}
