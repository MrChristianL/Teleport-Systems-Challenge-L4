package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// identityToRole maps certificate identities (SANs) to roles
var identityToRole = map[string]string{
	"admin.example.com": "admin",
	"user.example.com":  "user",
	// Add more identities here to assign them to roles
}

// rolePermissions defines the RPCs each role is allowed to call
var rolePermissions = map[string][]string{
	"admin": { // admin role: full access
		"/jobctl.JobService/StartJob",
		"/jobctl.JobService/StopJob",
		"/jobctl.JobService/GetStatus",
		"/jobctl.JobService/StreamOutput",
	},
	"user": { // user role: read-only access
		"/jobctl.JobService/GetStatus",
		"/jobctl.JobService/StreamOutput",
	},
	// any other role (e.g. unknown): no access
}

// extractRole maps an identity (SAN) to its assigned role
func extractRole(identity string) string {
	role, ok := identityToRole[identity]
	if !ok {
		return "unknown"
	}
	return role
}

func isAuthorized(role string, method string) bool {
	allowedMethods, ok := rolePermissions[role]
	if !ok {
		return false // role has no permissions defined
	}

	for _, allowed := range allowedMethods {
		if allowed == method {
			return true
		}
	}
	return false
}

// Extract SANs from context (DNS-only)
func extractSANs(ctx context.Context) []string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil
	}

	if len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return nil
	}

	return tlsInfo.State.VerifiedChains[0][0].DNSNames
}

func UnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	sans := extractSANs(ctx)
	if len(sans) == 0 {
		return nil, status.Error(codes.Unauthenticated, "no valid certificate")
	}

	// Use the first SAN as the client identity
	identity := sans[0]
	role := extractRole(identity)
	if role == "unknown" {
		return nil, status.Error(codes.Unauthenticated, "unknown identity")
	}

	if !isAuthorized(role, info.FullMethod) {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}

	return handler(ctx, req)
}

func StreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	sans := extractSANs(ss.Context())
	if len(sans) == 0 {
		return status.Error(codes.Unauthenticated, "no valid certificate")
	}

	// Use the first SAN as the client identity
	identity := sans[0]
	role := extractRole(identity)
	if role == "unknown" {
		return status.Error(codes.Unauthenticated, "unknown identity")
	}

	if !isAuthorized(role, info.FullMethod) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}

	return handler(srv, ss)
}

// TODO:
// ---
// For production, consider supporting IP SANs (tlsInfo.State.VerifiedChains[0][0].IPAddresses)
// or URI SANs along with DNS if sticking with SANs auth
