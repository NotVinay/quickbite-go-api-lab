package main

import (
	"context"
	"io"
	"log"
	"math"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "quickbite/07-grpc/pb"
)

const (
	Port             = ":9090"
	MetersPerMinute  = 250.0 // ~15 km/h in Bengaluru traffic
)

type Trip struct {
	riderName  string
	lat        float64
	lng        float64
	metersLeft int32
}

var trips = map[string]Trip{
	"ord_1": {"Ravi", 12.9352, 77.6245, 2400},
	"ord_2": {"Imran", 12.9716, 77.5946, 900},
}

type DeliveryServer struct {
	pb.UnimplementedDeliveryServiceServer
}

// 1. Unary RPC: GetEta
func (s *DeliveryServer) GetEta(ctx context.Context, req *pb.EtaRequest) (*pb.EtaReply, error) {
	trip, exists := trips[req.GetOrderId()]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "No trip for %s", req.GetOrderId())
	}

	minutes := int32(math.Ceil(float64(trip.metersLeft) / MetersPerMinute))
	return &pb.EtaReply{
		OrderId:   req.GetOrderId(),
		Minutes:   minutes,
		RiderName: trip.riderName,
	}, nil
}

// 2. Server Streaming RPC: TrackRider
func (s *DeliveryServer) TrackRider(req *pb.TrackRequest, stream pb.DeliveryService_TrackRiderServer) error {
	trip, exists := trips[req.GetOrderId()]
	if !exists {
		return status.Errorf(codes.NotFound, "No trip for %s", req.GetOrderId())
	}

	for i := 0; i < 6; i++ {
		// Detect client disconnect or deadline cancellation
		if err := stream.Context().Err(); err != nil {
			return err
		}

		fraction := float64(i) / 5.0
		metersLeft := int32(float64(trip.metersLeft) * (1.0 - fraction))
		lat := trip.lat + (float64(i) * 0.0004)
		lng := trip.lng + (float64(i) * 0.0004)

		loc := &pb.RiderLocation{
			OrderId:     req.GetOrderId(),
			Lat:         lat,
			Lng:         lng,
			MetersLeft:  metersLeft,
			TimestampMs: time.Now().UnixMilli(),
		}

		if err := stream.Send(loc); err != nil {
			return err
		}

		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// 3. Client Streaming RPC: UploadRoute
func (s *DeliveryServer) UploadRoute(stream pb.DeliveryService_UploadRouteServer) error {
	var count int32
	var totalKm float64
	var prev *pb.RiderLocation

	for {
		loc, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.RouteSummary{
				Points:  count,
				TotalKm: totalKm,
			})
		}
		if err != nil {
			return err
		}

		count++
		if prev != nil {
			totalKm += haversineKm(prev.Lat, prev.Lng, loc.Lat, loc.Lng)
		}
		prev = loc
	}
}

// Haversine formula to calculate distance in km between two GPS coordinates
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*(math.Pi/180.0))*math.Cos(lat2*(math.Pi/180.0))*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return 6371 * c
}

func main() {
	listener, err := net.Listen("tcp", Port)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", Port, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterDeliveryServiceServer(grpcServer, &DeliveryServer{})

	log.Printf("QuickBite gRPC DeliveryService (Go) listening on localhost%s (HTTP/2, protobuf)", Port)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve gRPC: %v", err)
	}
}
