package cmdutil

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/buzzhpc/buzz-cli/internal/client"
	"github.com/buzzhpc/buzz-cli/internal/output"
)

// WaitForReady polls the resource at path until status is terminal or timeout.
func WaitForReady(ctx context.Context, c *client.Client, path string, label string) error {
	start := time.Now()
	interval := 5 * time.Second
	timeout := 15 * time.Minute

	for {
		elapsed := time.Since(start).Round(time.Second)
		b, err := c.Get(ctx, path)
		if err != nil {
			return fmt.Errorf("polling failed: %w", err)
		}
		var res client.CommonResource
		if json.Unmarshal(b, &res) == nil {
			status := output.ExtractStatus(res.Status)
			switch status {
			case "success", "deployed", "running", "active":
				output.Success(fmt.Sprintf("%s is ready. (%s)", label, elapsed))
				return nil
			case "failed", "error":
				return fmt.Errorf("%s failed to deploy (status: %s)", label, status)
			}
		}
		if time.Since(start) > timeout {
			return fmt.Errorf("timed out waiting for %s to be ready", label)
		}
		output.Info(fmt.Sprintf("Waiting for %s to be ready... (%s)", label, elapsed))
		time.Sleep(interval)
	}
}
