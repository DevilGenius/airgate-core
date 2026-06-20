package plugin

import (
	"context"
	"testing"
)

func TestStartExtensionAndMiddlewareRuntimeWithBufconnClients(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(t.TempDir(), "debug", "", nil)
	t.Cleanup(manager.devWatcher.Close)

	gatewayClient, cleanupGateway := newGatewayRuntimeClient(t, &pluginRuntimeGateway{
		id:       "runtime-gateway",
		platform: "openai",
	})
	defer cleanupGateway()

	gatewayName, err := manager.startGatewayPlugin(ctx, nil, gatewayClient, "requested-gateway", "gateway-bin")
	if err != nil {
		t.Fatalf("startGatewayPlugin() error = %v", err)
	}
	if gatewayName != "runtime-gateway" {
		t.Fatalf("gateway canonical name = %q", gatewayName)
	}
	gatewayInst := manager.GetInstance("requested-gateway")
	if gatewayInst == nil || gatewayInst.Gateway == nil || gatewayInst.Platform != "openai" || gatewayInst.SourceName != "requested-gateway" {
		t.Fatalf("gateway instance = %+v", gatewayInst)
	}

	extensionClient, cleanupExtension := newExtensionRuntimeClient(t, &pluginRuntimeExtension{id: "runtime-extension"})
	defer cleanupExtension()

	extensionName, err := manager.startExtensionPlugin(ctx, nil, extensionClient, "requested-extension", "extension-bin")
	if err != nil {
		t.Fatalf("startExtensionPlugin() error = %v", err)
	}
	if extensionName != "runtime-extension" {
		t.Fatalf("extension canonical name = %q", extensionName)
	}
	extensionInst := manager.GetInstance("requested-extension")
	if extensionInst == nil || extensionInst.Extension == nil || extensionInst.SourceName != "requested-extension" || extensionInst.BinaryDir != "extension-bin" {
		t.Fatalf("extension instance = %+v", extensionInst)
	}
	if manager.GetExtensionByName("extension-bin") == nil {
		t.Fatal("extension binary alias did not resolve")
	}

	middlewareClient, cleanupMiddleware := newMiddlewareRuntimeClient(t, &pluginRuntimeMiddleware{id: "runtime-middleware"})
	defer cleanupMiddleware()

	middlewareName, err := manager.startMiddlewarePlugin(ctx, nil, middlewareClient, "requested-middleware", "middleware-bin")
	if err != nil {
		t.Fatalf("startMiddlewarePlugin() error = %v", err)
	}
	if middlewareName != "runtime-middleware" {
		t.Fatalf("middleware canonical name = %q", middlewareName)
	}
	middlewareInst := manager.GetInstance("middleware-bin")
	if middlewareInst == nil || middlewareInst.Middleware == nil || middlewareInst.Type != "middleware" || middlewareInst.SourceName != "requested-middleware" {
		t.Fatalf("middleware instance = %+v", middlewareInst)
	}
}
