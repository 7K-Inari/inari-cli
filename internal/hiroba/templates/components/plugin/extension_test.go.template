package scaffold

import (
	"context"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/testkit"
)

func TestPublicContract(t *testing.T) {
	client := testkit.Run(t, New())
	ctx := context.Background()
	info, err := client.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.GetName() != Name || info.GetApiVersion() != "inari.plugin.v1" {
		t.Fatalf("unexpected info: %+v", info)
	}
	actions, err := client.Capabilities(ctx)
	if err != nil || len(actions) != 1 || actions[0].GetName() != "scaffold.ping" {
		t.Fatalf("unexpected capabilities: %+v, %v", actions, err)
	}
	response, err := client.Invoke(ctx, "scaffold.ping", pluginsdk.AuthContext{PrincipalID: "ci", TenantID: "org:ci"}, nil)
	if err != nil || string(response.Result) != `{"status":"ok"}` {
		t.Fatalf("unexpected response: %+v, %v", response, err)
	}
	serving, _, err := client.Health(ctx)
	if err != nil || !serving {
		t.Fatalf("health: serving=%v err=%v", serving, err)
	}
}
