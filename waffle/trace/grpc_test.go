package trace

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"gitlab.com/gorib/failure"
	waffleGrpc "gitlab.com/gorib/waffle/tools/grpc"

	record "github.com/specrig/specrig-go/trace"
)

func TestARefusalIsNamedByItsCodeAndTypeNotItsText(t *testing.T) {
	onWire := func(id string) error {
		st, _ := status.New(codes.NotFound, "contract "+id+" not found").WithDetails(&errdetails.ErrorInfo{
			Reason: "CONTRACT_NOT_FOUND", Domain: failure.Kind,
		})
		return st.Err()
	}
	for _, err := range []error{onWire("55"), onWire("56")} {
		if got := refusal(err); got != "NotFound contract_not_found" {
			t.Errorf("refusal(%v) = %q", err, got)
		}
	}
	if got := refusal(errors.New("boom")); got != "failed" {
		t.Errorf("a plain error is %q, want failed", got)
	}
}

type neighbour struct{}

func (neighbour) StreamingOutputCall(_ context.Context, in *testpb.StreamingOutputCallRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[testpb.StreamingOutputCallResponse], error) {
	if len(in.ResponseParameters) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no sizes")
	}
	out := &replies{}
	for _, p := range in.ResponseParameters {
		out.left = append(out.left, sized(p.Size))
	}
	return out, nil
}

func (neighbour) StreamingInputCall(context.Context, ...grpc.CallOption) (grpc.ClientStreamingClient[testpb.StreamingInputCallRequest, testpb.StreamingInputCallResponse], error) {
	return &upload{}, nil
}

func (neighbour) FullDuplexCall(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[testpb.StreamingOutputCallRequest, testpb.StreamingOutputCallResponse], error) {
	return &replies{}, nil
}

type replies struct {
	grpc.ClientStream
	left []*testpb.StreamingOutputCallResponse
}

func (r *replies) Send(in *testpb.StreamingOutputCallRequest) error {
	r.left = append(r.left, sized(in.ResponseParameters[0].Size))
	return nil
}

func (r *replies) CloseSend() error { return nil }

func (r *replies) Recv() (*testpb.StreamingOutputCallResponse, error) {
	if len(r.left) == 0 {
		return nil, io.EOF
	}
	next := r.left[0]
	r.left = r.left[1:]
	return next, nil
}

type upload struct {
	grpc.ClientStream
	size int32
}

func (u *upload) Send(in *testpb.StreamingInputCallRequest) error {
	u.size += int32(len(in.Payload.Body))
	return nil
}

func (u *upload) CloseAndRecv() (*testpb.StreamingInputCallResponse, error) {
	return &testpb.StreamingInputCallResponse{AggregatedPayloadSize: u.size}, nil
}

func sized(size int32) *testpb.StreamingOutputCallResponse {
	return &testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{Body: make([]byte, size)}}
}

func asking(sizes ...int32) *testpb.StreamingOutputCallRequest {
	in := &testpb.StreamingOutputCallRequest{}
	for _, size := range sizes {
		in.ResponseParameters = append(in.ResponseParameters, &testpb.ResponseParameters{Size: size})
	}
	return in
}

func TestAReplacedServerStreamIsReadToItsEnd(t *testing.T) {
	Enable("shop")
	client := testpb.NewTestServiceClient(Outbound(neighbour{}))

	stream, err := client.StreamingOutputCall(t.Context(), asking(1, 2))
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for {
		reply, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, len(reply.Payload.Body))
	}

	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("read %v, want [1 2]", got)
	}
	r := recorded(t, "out", testpb.TestService_StreamingOutputCall_FullMethodName)
	if sent, received := count(t, r.Request), count(t, r.Response); sent != 1 || received != 2 {
		t.Errorf("recorded %d sent and %d received, want 1 and 2", sent, received)
	}
}

func TestAReplacedServerStreamRefusesWhereTheCallIsMade(t *testing.T) {
	Enable("shop")
	client := testpb.NewTestServiceClient(Outbound(neighbour{}))

	_, err := client.StreamingOutputCall(t.Context(), asking())

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err = %v, want InvalidArgument", err)
	}
	if r := recorded(t, "out", testpb.TestService_StreamingOutputCall_FullMethodName); r.Error != "InvalidArgument" {
		t.Errorf("recorded the refusal as %q", r.Error)
	}
}

func TestAReplacedClientStreamAnswersOnceItIsClosed(t *testing.T) {
	Enable("shop")
	client := testpb.NewTestServiceClient(Outbound(neighbour{}))

	stream, err := client.StreamingInputCall(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{3, 4} {
		if err := stream.Send(&testpb.StreamingInputCallRequest{Payload: &testpb.Payload{Body: make([]byte, size)}}); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}

	if reply.AggregatedPayloadSize != 7 {
		t.Errorf("aggregated %d, want 7", reply.AggregatedPayloadSize)
	}
	r := recorded(t, "out", testpb.TestService_StreamingInputCall_FullMethodName)
	if sent, received := count(t, r.Request), count(t, r.Response); sent != 2 || received != 1 {
		t.Errorf("recorded %d sent and %d received, want 2 and 1", sent, received)
	}
}

func TestAReplacedBidiStreamPassesBothWays(t *testing.T) {
	Enable("shop")
	client := testpb.NewTestServiceClient(Outbound(neighbour{}))

	stream, err := client.FullDuplexCall(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(asking(5)); err != nil {
		t.Fatal(err)
	}
	reply, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("after close: %v, want EOF", err)
	}

	if len(reply.Payload.Body) != 5 {
		t.Errorf("echoed %d bytes, want 5", len(reply.Payload.Body))
	}
	r := recorded(t, "out", testpb.TestService_FullDuplexCall_FullMethodName)
	if sent, received := count(t, r.Request), count(t, r.Response); sent != 1 || received != 1 {
		t.Errorf("recorded %d sent and %d received, want 1 and 1", sent, received)
	}
}

type sizes struct {
	testpb.UnimplementedTestServiceServer
}

func (s sizes) Start(registrar grpc.ServiceRegistrar) { testpb.RegisterTestServiceServer(registrar, s) }

func (sizes) StreamingOutputCall(in *testpb.StreamingOutputCallRequest, stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
	for _, p := range in.ResponseParameters {
		if err := stream.Send(sized(p.Size)); err != nil {
			return err
		}
	}
	return nil
}

func (sizes) StreamingInputCall(stream grpc.ClientStreamingServer[testpb.StreamingInputCallRequest, testpb.StreamingInputCallResponse]) error {
	var total int32
	for {
		in, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&testpb.StreamingInputCallResponse{AggregatedPayloadSize: total})
		}
		if err != nil {
			return err
		}
		total += int32(len(in.Payload.Body))
	}
}

func serve(t *testing.T) testpb.TestServiceClient {
	server, err := waffleGrpc.Serve([]waffleGrpc.ServiceHandler{sizes{}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return testpb.NewTestServiceClient(Inbound(server.Conn()))
}

func TestAServerStreamToTheServiceIsRecordedWhenItEnds(t *testing.T) {
	Enable("shop")
	client := serve(t)

	stream, err := client.StreamingOutputCall(t.Context(), asking(1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := stream.Recv(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}

	r := recorded(t, "in", testpb.TestService_StreamingOutputCall_FullMethodName)
	if sent, received := count(t, r.Request), count(t, r.Response); sent != 1 || received != 3 {
		t.Errorf("recorded %d sent and %d received, want 1 and 3", sent, received)
	}
}

func TestAClientStreamToTheServiceIsRecordedOnItsAnswer(t *testing.T) {
	Enable("shop")
	client := serve(t)

	stream, err := client.StreamingInputCall(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1, 2} {
		if err := stream.Send(&testpb.StreamingInputCallRequest{Payload: &testpb.Payload{Body: make([]byte, size)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatal(err)
	}

	r := recorded(t, "in", testpb.TestService_StreamingInputCall_FullMethodName)
	if sent, received := count(t, r.Request), count(t, r.Response); sent != 2 || received != 1 {
		t.Errorf("recorded %d sent and %d received, want 2 and 1", sent, received)
	}
}

func recorded(t *testing.T, dir, name string) Record {
	t.Helper()
	records := Records()
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Dir == dir && records[i].Name == name {
			return records[i]
		}
	}
	t.Fatalf("no %s record of %s", dir, name)
	return record.Record{}
}

func count(t *testing.T, body json.RawMessage) int {
	t.Helper()
	var messages []json.RawMessage
	if err := json.Unmarshal(body, &messages); err != nil {
		t.Fatalf("not a list of messages: %s", body)
	}
	return len(messages)
}
