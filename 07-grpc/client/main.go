package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "quickbite/07-grpc/pb"
)

func main() {
	conn, err := grpc.NewClient("localhost:9090", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect to gRPC server: %v", err)
	}
	defer conn.Close()

	client := pb.NewDeliveryServiceClient(conn)

	fmt.Println("=== 1. Unary: GetEta ===")
	runUnary(client)

	fmt.Println("\n=== 2. Server streaming: TrackRider ===")
	runServerStreaming(client)

	fmt.Println("\n=== 3. Client streaming: UploadRoute (stretch) ===")
	runClientStreaming(client)
}

func runUnary(client pb.DeliveryServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// a) Normal call
	reply, err := client.GetEta(ctx, &pb.EtaRequest{OrderId: "ord_1"})
	if err != nil {
		log.Fatalf("GetEta failed: %v", err)
	}
	fmt.Printf("%s arrives in %d min\n", reply.GetRiderName(), reply.GetMinutes())

	// b) Compare wire size: Protobuf vs JSON
	protoBytes, _ := proto.Marshal(reply)
	jsonBytes, _ := json.Marshal(map[string]any{
		"order_id":   reply.GetOrderId(),
		"minutes":    reply.GetMinutes(),
		"rider_name": reply.GetRiderName(),
	})
	fmt.Printf("wire size: protobuf %d bytes vs JSON %d bytes\n", len(protoBytes), len(jsonBytes))

	// c) Error handling for non-existent order
	ctxErr, cancelErr := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelErr()

	_, err = client.GetEta(ctxErr, &pb.EtaRequest{OrderId: "ord_999"})
	if err != nil {
		st, _ := status.FromError(err)
		fmt.Printf("ord_999 -> %s: %s\n", st.Code(), st.Message())
	}
}

func runServerStreaming(client pb.DeliveryServiceClient) {
	// Stream ord_1
	stream, err := client.TrackRider(context.Background(), &pb.TrackRequest{OrderId: "ord_1"})
	if err != nil {
		log.Fatalf("TrackRider failed: %v", err)
	}

	for {
		loc, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("stream receive error: %v", err)
		}
		fmt.Printf("  %4d m left  (%.4f, %.4f)\n", loc.GetMetersLeft(), loc.GetLat(), loc.GetLng())
	}

	// Stream ord_2 with a 1-second deadline (simulating deadline exceeded mid-stream)
	ctxDead, cancelDead := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelDead()

	stream2, err := client.TrackRider(ctxDead, &pb.TrackRequest{OrderId: "ord_2"})
	if err == nil {
		for {
			_, err := stream2.Recv()
			if err != nil {
				st, _ := status.FromError(err)
				fmt.Printf("  ord_2 stream ended with %s (deadline hit mid-stream)\n", st.Code())
				break
			}
		}
	}
}

func runClientStreaming(client pb.DeliveryServiceClient) {
	stream, err := client.UploadRoute(context.Background())
	if err != nil {
		log.Fatalf("UploadRoute failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		loc := &pb.RiderLocation{
			OrderId: "ord_1",
			Lat:     12.9352 + float64(i)*0.003,
			Lng:     77.6245 + float64(i)*0.003,
		}
		_ = stream.Send(loc)
	}

	summary, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("CloseAndRecv failed: %v", err)
	}
	fmt.Printf("  server says: %d points, %.2f km\n", summary.GetPoints(), summary.GetTotalKm())
}

