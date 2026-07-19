// Package xray is a thin client for Xray's gRPC StatsService with
// graceful degradation across Xray versions.
package xray

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/wlix13/orrery/collector/internal/xray/command"
)

// DialFunc opens a connection to the node's Xray API listener.
// It abstracts over direct TCP and SSH-tunneled transports.
type DialFunc func(ctx context.Context, addr string) (net.Conn, error)

type Stat struct {
	Name  string
	Value int64
}

type OnlineIP struct {
	IP       string
	LastSeen int64 // unix seconds
}

type OnlineUser struct {
	Email string
	IPs   []OnlineIP // may be empty when the node's Xray predates IP-list RPCs
}

type SysStats struct {
	NumGoroutine uint32
	NumGC        uint32
	Alloc        uint64
	Sys          uint64
	UptimeS      uint32
}

// Client wraps one gRPC connection to one node. It is safe to keep across
// polls; the underlying connection reconnects transparently, and the
// dialer decides how the wire is established.
type Client struct {
	conn *grpc.ClientConn
	svc  command.StatsServiceClient
}

// New creates the client. No I/O happens until the first RPC.
func New(addr string, dial DialFunc) (*Client, error) {
	conn, err := grpc.NewClient(
		"passthrough:///"+addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, target string) (net.Conn, error) {
			return dial(ctx, target)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc client for %s: %w", addr, err)
	}

	return &Client{conn: conn, svc: command.NewStatsServiceClient(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// QueryAll returns every stat counter (non-destructive read).
func (c *Client) QueryAll(ctx context.Context) ([]Stat, error) {
	resp, err := c.svc.QueryStats(ctx, &command.QueryStatsRequest{Pattern: "", Reset_: false})
	if err != nil {
		return nil, fmt.Errorf("QueryStats: %w", err)
	}

	stats := make([]Stat, 0, len(resp.Stat))
	for _, s := range resp.Stat {
		stats = append(stats, Stat{Name: s.Name, Value: s.Value})
	}

	return stats, nil
}

func (c *Client) SysStats(ctx context.Context) (*SysStats, error) {
	resp, err := c.svc.GetSysStats(ctx, &command.SysStatsRequest{})
	if err != nil {
		return nil, fmt.Errorf("GetSysStats: %w", err)
	}

	return &SysStats{
		NumGoroutine: resp.NumGoroutine,
		NumGC:        resp.NumGC,
		Alloc:        resp.Alloc,
		Sys:          resp.Sys,
		UptimeS:      resp.Uptime,
	}, nil
}

// OnlineUsers returns currently-online users. The second return is false
// when the node's Xray is too old to expose any online-user RPC - the
// caller should skip online tracking rather than treat it as a failure.
//
// Preference order: GetUsersStats (one RPC, Xray ≥ 2026-04) →
// GetAllOnlineUsers + per-user GetStatsOnlineIpList (≥ 2025-02) →
// GetAllOnlineUsers alone (≥ 2025-12... emails only) → unsupported.
func (c *Client) OnlineUsers(ctx context.Context) ([]OnlineUser, bool, error) {
	if users, err := c.onlineViaUsersStats(ctx); err == nil {
		return users, true, nil
	} else if !isUnimplemented(err) {
		return nil, true, err
	}

	resp, err := c.svc.GetAllOnlineUsers(ctx, &command.GetAllOnlineUsersRequest{})
	if err != nil {
		if isUnimplemented(err) {
			return nil, false, nil
		}

		return nil, true, fmt.Errorf("GetAllOnlineUsers: %w", err)
	}

	users := make([]OnlineUser, 0, len(resp.Users))
	ipListSupported := true

	for _, email := range resp.Users {
		u := OnlineUser{Email: email}

		if ipListSupported {
			ips, err := c.svc.GetStatsOnlineIpList(ctx, &command.GetStatsRequest{Name: "user>>>" + email + ">>>online"})

			switch {
			case err == nil:
				for ip, seen := range ips.Ips {
					u.IPs = append(u.IPs, OnlineIP{IP: ip, LastSeen: seen})
				}
			case isUnimplemented(err):
				ipListSupported = false // keep emails, drop IP detail
			case isNotFound(err):
				// user went offline between the two RPCs; keep the email
			default:
				return nil, true, fmt.Errorf("GetStatsOnlineIpList(%s): %w", email, err)
			}
		}

		users = append(users, u)
	}

	return users, true, nil
}

func (c *Client) onlineViaUsersStats(ctx context.Context) ([]OnlineUser, error) {
	resp, err := c.svc.GetUsersStats(ctx, &command.GetUsersStatsRequest{IncludeTraffic: false, Reset_: false})
	if err != nil {
		return nil, err
	}

	users := make([]OnlineUser, 0, len(resp.Users))

	for _, u := range resp.Users {
		ou := OnlineUser{Email: u.Email}
		for _, ip := range u.Ips {
			ou.IPs = append(ou.IPs, OnlineIP{IP: ip.Ip, LastSeen: ip.LastSeen})
		}

		users = append(users, ou)
	}

	return users, nil
}

func isUnimplemented(err error) bool {
	return status.Code(err) == codes.Unimplemented
}

func isNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
