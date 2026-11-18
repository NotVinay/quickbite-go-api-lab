package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "quickbite/07-grpc/pb"
)

func TestGRPCServer(t *testing.T) {
	// Create an in-memory listener
	buffer := 1024 * 1024
	lis := bufconn.Listen(buffer)

	srv := grpc.NewServer()
	pb.RegisterDeliveryServiceServer(srv, &DeliveryServer{})
	go func() {
		_ = srv.Serve(lis)
	}()
	defer srv.Stop()

	// Dial in-memory server
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := pb.NewDeliveryServiceClient(conn)

	// 1. Unary RPC
	t.Run("Unary: GetEta", func(t *testing.T) {
		reply, err := client.GetEta(context.Background(), &pb.EtaRequest{OrderId: "ord_1"})
		if err != nil {
			t.Fatalf("GetEta failed: %v", err)
		}
		if reply.RiderName != "Ravi" || reply.Minutes != 10 {
			t.Fatalf("unexpected reply: %+v", reply)
		}

		// Non-existent order -> codes.NotFound
		_, err = client.GetEta(context.Background(), &pb.EtaRequest{OrderId: "ord_999"})
		if err == nil {
			t.Fatalf("expected error for ord_999")
		}
		st, _ := status.FromError(err)
		if st.Code() != codes.NotFound {
			t.Fatalf("expected NotFound code, got %s", st.Code())
		}
	})

	// 2. Server Streaming RPC
	t.Run("Server Streaming: TrackRider", func(t *testing.T) {
		stream, err := client.TrackRider(context.Background(), &pb.TrackRequest{OrderId: "ord_2"})
		if err != nil {
			t.Fatalf("TrackRider failed: %v", err)
		}

		var points []*pb.RiderLocation
		for {
			loc, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("stream recv error: %v", err)
			}
			points = append(points, loc)
		}

		if len(points) != 6 {
			t.Fatalf("expected 6 points, got %d", len(points))
		}
		if points[len(points)-1].MetersLeft != 0 {
			t.Fatalf("expected final metersLeft to be 0, got %d", points[len(points)-1].MetersLeft)
		}
	})

	// 3. Client Streaming RPC
	t.Run("Client Streaming: UploadRoute", func(t *testing.T) {
		stream, err := client.UploadRoute(context.Background())
		if err != nil {
			t.Fatalf("UploadRoute failed: %v", err)
		}

		for i := 0; i < 4; i++ {
			_ = stream.Send(&pb.RiderLocation{
				OrderId: "ord_1",
				Lat:     12.9352 + float64(i)*0.003,
				Lng:     77.6245 + float64(i)*0.003,
			})
		}

		summary, err := stream.CloseAndRecv()
		if err != nil {
			t.Fatalf("CloseAndRecv failed: %v", err)
		}
		if summary.Points != 4 || summary.TotalKm <= 0 {
			t.Fatalf("unexpected summary: %+v", summary)
		}
	})

	// 4. Deadline / Timeout test
	t.Run("Deadline Cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		stream, err := client.TrackRider(ctx, &pb.TrackRequest{OrderId: "ord_1"})
		if err != nil {
			t.Fatalf("TrackRider failed: %v", err)
		}

		var finalErr error
		for {
			_, err := stream.Recv()
			if err != nil {
				finalErr = err
				break
			}
		}

		st, _ := status.FromError(finalErr)
		if st.Code() != codes.DeadlineExceeded {
			t.Fatalf("expected DeadlineExceeded code, got %s", st.Code())
		}
	})
}

