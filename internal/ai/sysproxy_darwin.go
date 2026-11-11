package ai

import (
	"context"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/execx"
)

func init() {
	readSystemProxy = func() (sysProxy, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := execx.Command(ctx, "scutil", "--proxy").Output()
		if err != nil {
			return sysProxy{}, false
		}
		return parseScutil(string(out)), true
	}
}
