package tunnel

import "testing"

func TestIsForwardedWebSocket(t *testing.T) {
	cases := []struct {
		name string
		req  *requestData
		want bool
	}{
		{
			name: "flat headers websocket",
			req: &requestData{
				Headers: map[string]string{
					"Upgrade":    "websocket",
					"Connection": "Upgrade",
				},
			},
			want: true,
		},
		{
			name: "headers_v2 websocket",
			req: &requestData{
				HeadersV2: map[string][]string{
					"Upgrade":    {"websocket"},
					"Connection": {"keep-alive", "Upgrade"},
				},
			},
			want: true,
		},
		{
			name: "plain get",
			req: &requestData{
				Headers: map[string]string{"Accept": "text/html"},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isForwardedWebSocket(tc.req); got != tc.want {
				t.Fatalf("isForwardedWebSocket() = %v, want %v", got, tc.want)
			}
		})
	}
}
