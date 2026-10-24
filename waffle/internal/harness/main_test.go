package harness

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitlab.com/gorib/env"
	waffle "gitlab.com/gorib/waffle/container"
	waffleHttp "gitlab.com/gorib/waffle/tools/http"
)

const grpcContainer = `package diogen

import (
	"time"

	env "gitlab.com/gorib/env"
	grpcClient "gitlab.com/gorib/grpc/client"
	server "gitlab.com/gorib/grpc/server"
	pry "gitlab.com/gorib/pry"
	sql "gitlab.com/gorib/sql"
	grpcCommand "gitlab.com/gorib/waffle/tools/grpc"
	contractProto "example.test/api/contract"
	shopGrpc "example.test/shop/domain/shop/cmd/grpc"
	baseGrpc "google.golang.org/grpc"
)

type Replacements struct {
	SqlDb                                                            sql.Db
	PryLogger                                                        pry.Logger
	BaseGrpcClientConnInterfaceForContractProtoContractServiceClient baseGrpc.ClientConnInterface
	ContractProtoContractServiceClient                               contractProto.ContractServiceClient
	ShopThing                                                        shopGrpc.Thing
	GrpcCommandServiceHandlerAsServiceTag                            []grpcCommand.ServiceHandler
}

// Diogen assigns every tagged collection to a field of its own and passes the
// field. A fixture that inlined the literals would test a container shape the
// generator does not produce.
type Application struct {
	sqlDb                                                            sql.Db
	pryLogger                                                        pry.Logger
	baseGrpcClientConnInterfaceForContractProtoContractServiceClient baseGrpc.ClientConnInterface
	contractProtoContractServiceClient                               contractProto.ContractServiceClient
	shopThing                                                        shopGrpc.Thing
	grpcCommandServiceHandlerAsServiceTag                            []grpcCommand.ServiceHandler
	serverInterceptorAsMiddlewareTag                                 []server.Interceptor
	serverStreamInterceptorAsStreamMiddlewareTag                     []server.StreamInterceptor
}

func InitAppWith(replacements Replacements) (*Application, error) {
	container.sqlDb = replacements.SqlDb

	if container.sqlDb == nil {
		s, err := sql.NewDb(env.NeedValue[string]("DB_DRIVER"), env.NeedValue[string]("DB_DSN"))
		if err != nil {
			return nil, err
		}
		container.sqlDb = s
	}

	container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient = replacements.BaseGrpcClientConnInterfaceForContractProtoContractServiceClient

	if container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient == nil {
		s, err := grpcClient.New(env.NeedValue[string]("DUNGEON_HOST"), env.NeedValue[string]("DUNGEON_PORT"))
		if err != nil {
			return nil, err
		}
		container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient = s
	}

	container.contractProtoContractServiceClient = replacements.ContractProtoContractServiceClient

	if container.contractProtoContractServiceClient == nil {
		s := contractProto.NewContractServiceClient(container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient)
		container.contractProtoContractServiceClient = s
	}

	container.shopThing = replacements.ShopThing

	if container.shopThing == nil {
		s := shopGrpc.NewThing(env.NeedValue[string]("THING_URL"), env.NeedValue[uint16]("THING_PORT"), env.NeedValue[time.Duration]("THING_TTL"), env.NeedJson[shopGrpc.Config]("THING_CONFIG"),
			env.NeedArray[int]("THING_SHARDS"), env.Value("LOGLEVEL", "info"), env.Value("POLL", 5*time.Second), env.Array("ORIGINS", []string{}),
			env.Value("LOCAL", shopGrpc.Fallback), env.Value("SQL_DSN", "x"), env.NeedValue[string]("Custom"))
		container.shopThing = s
	}
	if container.sqlDb == nil {
		_ = env.Value("SQL_DSN", "x")
	}

	container.grpcCommandServiceHandlerAsServiceTag = replacements.GrpcCommandServiceHandlerAsServiceTag

	if container.grpcCommandServiceHandlerAsServiceTag == nil {
		s := shopGrpc.NewOrderService(container.sqlDb)
		container.shopGrpcOrderServiceAsGrpcCommandServiceHandler = s
	}
	if container.grpcCommandServiceHandlerAsServiceTag == nil {
		container.grpcCommandServiceHandlerAsServiceTag = []grpcCommand.ServiceHandler{container.shopGrpcOrderServiceAsGrpcCommandServiceHandler}
	}
	container.serverInterceptorAsMiddlewareTag = []server.Interceptor{container.interceptorLogMWAsServerInterceptor}
	container.serverStreamInterceptorAsStreamMiddlewareTag = []server.StreamInterceptor{container.interceptorStreamLogMWAsServerStreamInterceptor}
	{
		s := grpcCommand.NewGrpcCommand(
			container.grpcCommandServiceHandlerAsServiceTag,
			container.serverInterceptorAsMiddlewareTag,
			container.serverStreamInterceptorAsStreamMiddlewareTag,
			container.appBaseCommandForGrpcCommandGrpcCommand,
		)
		container.grpcGrpcCommand = s
	}
	return container, nil
}
`

// grpcContainer as diogen writes it since every registration builds in a diogenBuild closure.
const grpcClosureContainer = `package diogen

import (
	"time"

	env "gitlab.com/gorib/env"
	grpcClient "gitlab.com/gorib/grpc/client"
	server "gitlab.com/gorib/grpc/server"
	pry "gitlab.com/gorib/pry"
	sql "gitlab.com/gorib/sql"
	grpcCommand "gitlab.com/gorib/waffle/tools/grpc"
	contractProto "example.test/api/contract"
	shopGrpc "example.test/shop/domain/shop/cmd/grpc"
	baseGrpc "google.golang.org/grpc"
)

type Replacements struct {
	SqlDb                                                            sql.Db
	PryLogger                                                        pry.Logger
	BaseGrpcClientConnInterfaceForContractProtoContractServiceClient baseGrpc.ClientConnInterface
	ContractProtoContractServiceClient                               contractProto.ContractServiceClient
	ShopThing                                                        shopGrpc.Thing
	GrpcCommandServiceHandlerAsServiceTag                            []grpcCommand.ServiceHandler
}

type Application struct {
	sqlDb                                                            sql.Db
	pryLogger                                                        pry.Logger
	baseGrpcClientConnInterfaceForContractProtoContractServiceClient baseGrpc.ClientConnInterface
	contractProtoContractServiceClient                               contractProto.ContractServiceClient
	shopThing                                                        shopGrpc.Thing
	grpcCommandServiceHandlerAsServiceTag                            []grpcCommand.ServiceHandler
	serverInterceptorAsMiddlewareTag                                 []server.Interceptor
	serverStreamInterceptorAsStreamMiddlewareTag                     []server.StreamInterceptor
}

func InitAppWith(replacements Replacements) (*Application, error) {
	container.sqlDb = replacements.SqlDb
	container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient = replacements.BaseGrpcClientConnInterfaceForContractProtoContractServiceClient
	container.contractProtoContractServiceClient = replacements.ContractProtoContractServiceClient
	container.shopThing = replacements.ShopThing
	container.grpcCommandServiceHandlerAsServiceTag = replacements.GrpcCommandServiceHandlerAsServiceTag

	buildSqlDb := diogenBuild("gitlab.com/gorib/sql.NewDb", func(name string) error {
		if container.sqlDb == nil {
			s, err := sql.NewDb(env.NeedValue[string]("DB_DRIVER"), env.NeedValue[string]("DB_DSN"))
			if err != nil {
				return err
			}
			container.sqlDb = s
		}
		return nil
	})

	buildBaseGrpcClientConnInterfaceForContractProtoContractServiceClient := diogenBuild("gitlab.com/gorib/grpc/client.New", func(name string) error {
		if container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient == nil {
			s, err := grpcClient.New(env.NeedValue[string]("DUNGEON_HOST"), env.NeedValue[string]("DUNGEON_PORT"))
			if err != nil {
				return err
			}
			container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient = s
		}
		return nil
	})

	buildContractProtoContractServiceClient := diogenBuild("example.test/api/contract.NewContractServiceClient", func(name string) error {
		if container.contractProtoContractServiceClient == nil {
			if err := buildBaseGrpcClientConnInterfaceForContractProtoContractServiceClient(); err != nil {
				return err
			}
			s := contractProto.NewContractServiceClient(container.baseGrpcClientConnInterfaceForContractProtoContractServiceClient)
			container.contractProtoContractServiceClient = s
		}
		return nil
	})

	buildShopThing := diogenBuild("example.test/shop/domain/shop/cmd/grpc.NewThing", func(name string) error {
		if container.shopThing == nil {
			s := shopGrpc.NewThing(env.NeedValue[string]("THING_URL"), env.NeedValue[uint16]("THING_PORT"), env.NeedValue[time.Duration]("THING_TTL"), env.NeedJson[shopGrpc.Config]("THING_CONFIG"),
				env.NeedArray[int]("THING_SHARDS"), env.Value("LOGLEVEL", "info"), env.Value("POLL", 5*time.Second), env.Array("ORIGINS", []string{}),
				env.Value("LOCAL", shopGrpc.Fallback), env.Value("SQL_DSN", "x"), env.NeedValue[string]("Custom"))
			container.shopThing = s
		}
		return nil
	})

	buildShopGrpcOrderServiceAsGrpcCommandServiceHandler := diogenBuild("example.test/shop/domain/shop/cmd/grpc.NewOrderService", func(name string) error {
		if err := buildSqlDb(); err != nil {
			return err
		}
		s := shopGrpc.NewOrderService(container.sqlDb)
		container.shopGrpcOrderServiceAsGrpcCommandServiceHandler = s
		return nil
	})

	buildGrpcCommandServiceHandlerAsServiceTag := diogenBuild("di.Tags(grpcCommand.ServiceTag)", func(name string) error {
		if container.grpcCommandServiceHandlerAsServiceTag == nil {
			if err := buildShopGrpcOrderServiceAsGrpcCommandServiceHandler(); err != nil {
				return err
			}
			container.grpcCommandServiceHandlerAsServiceTag = []grpcCommand.ServiceHandler{container.shopGrpcOrderServiceAsGrpcCommandServiceHandler}
		}
		return nil
	})

	buildGrpcGrpcCommand := diogenBuild("gitlab.com/gorib/waffle/tools/grpc.NewGrpcCommand", func(name string) error {
		if err := diogenNeed(buildGrpcCommandServiceHandlerAsServiceTag, buildServerInterceptorAsMiddlewareTag); err != nil {
			return err
		}
		s := grpcCommand.NewGrpcCommand(
			container.grpcCommandServiceHandlerAsServiceTag,
			container.serverInterceptorAsMiddlewareTag,
			container.serverStreamInterceptorAsStreamMiddlewareTag,
			container.appBaseCommandForGrpcCommandGrpcCommand,
		)
		container.grpcGrpcCommand = s
		return nil
	})

	if err := buildGrpcGrpcCommand(); err != nil {
		return nil, err
	}
	return container, nil
}
`

const orderHandler = `package grpc

import (
	"google.golang.org/grpc"

	proto "example.test/api/order"
)

func NewOrderService(db any) *orderService {
	return &orderService{}
}

type orderService struct {
	proto.OrderServiceServer
}

func (s *orderService) Start(server grpc.ServiceRegistrar) {
	proto.RegisterOrderServiceServer(server, s)
}
`

const httpContainer = `package diogen

import (
	pry "gitlab.com/gorib/pry"
	baseGrpc "google.golang.org/grpc"
	httpCommand "gitlab.com/gorib/waffle/tools/http"
)

type Replacements struct {
	PryLogger                                                    pry.Logger
	BaseGrpcClientConnInterfaceForMediaProtoMediaServiceClient   baseGrpc.ClientConnInterface
}

type Application struct {
	pryLogger                   pry.Logger
	httpServerMux               server.Mux
	httpCommandGlobal           httpCommand.Global
	httpServerRouterAsRouterTag []server.Router
}

func InitAppWith(replacements Replacements) (*Application, error) {
	container.httpServerRouterAsRouterTag = []server.Router{container.mediaHttpRouterAsServerRouter}
	{
		s := httpCommand.NewHttpCommand(container.httpServerMux, container.httpCommandGlobal, container.httpServerRouterAsRouterTag, container.pryLogger)
		container.httpHttpCommand = s
	}
	return container, nil
}
`

const noGlobalContainer = `package diogen

import (
	pry "gitlab.com/gorib/pry"
	httpCommand "gitlab.com/gorib/waffle/tools/http"
)

type Replacements struct {
	PryLogger pry.Logger
}

type Application struct {
	pryLogger                   pry.Logger
	httpServerMux               server.Mux
	httpServerRouterAsRouterTag []server.Router
}

func InitAppWith(replacements Replacements) (*Application, error) {
	{
		s := httpCommand.NewHttpCommand(container.httpServerMux, container.httpServerRouterAsRouterTag, container.pryLogger)
		container.httpHttpCommand = s
	}
	return container, nil
}
`

const singleRouterContainer = `package diogen

import (
	pry "gitlab.com/gorib/pry"
	httpCommand "gitlab.com/gorib/waffle/tools/http"
)

type Replacements struct {
	PryLogger pry.Logger
}

type Application struct {
	pryLogger        pry.Logger
	httpServerMux    server.Mux
	httpServerRouter server.Router
}

func InitAppWith(replacements Replacements) (*Application, error) {
	{
		s := httpCommand.NewHttpCommand(container.httpServerMux, container.httpServerRouter, container.pryLogger)
		container.httpHttpCommand = s
	}
	return container, nil
}
`

const bothContainer = `package diogen

import (
	sql "gitlab.com/gorib/sql"
	pry "gitlab.com/gorib/pry"
	grpcCommand "gitlab.com/gorib/waffle/tools/grpc"
	httpCommand "gitlab.com/gorib/waffle/tools/http"
	server "gitlab.com/gorib/grpc/server"
	httpServer "gitlab.com/gorib/http/server"
)

type Replacements struct {
	SqlDb     sql.Db
	PryLogger pry.Logger
}

type Application struct {
	sqlDb                                 sql.Db
	pryLogger                             pry.Logger
	grpcCommandServiceHandlerAsServiceTag []grpcCommand.ServiceHandler
	serverInterceptorAsMiddlewareTag      []server.Interceptor
	httpServerMux                         server.Mux
	httpCommandGlobal                     httpCommand.Global
	httpServerRouterAsRouterTag           []httpServer.Router
}

func InitAppWith(replacements Replacements) (*Application, error) {
	container.grpcCommandServiceHandlerAsServiceTag = []grpcCommand.ServiceHandler{container.shopGrpcOrderServiceAsGrpcCommandServiceHandler}
	container.serverInterceptorAsMiddlewareTag = []server.Interceptor{container.interceptorLogMWAsServerInterceptor}
	{
		s := grpcCommand.NewGrpcCommand(
			container.grpcCommandServiceHandlerAsServiceTag,
			container.serverInterceptorAsMiddlewareTag,
			nil,
			container.appBaseCommandForGrpcCommandGrpcCommand,
		)
		container.grpcGrpcCommand = s
	}
	{
		s := httpCommand.NewHttpCommand(container.httpServerMux, container.httpCommandGlobal, container.httpServerRouterAsRouterTag, container.pryLogger)
		container.httpHttpCommand = s
	}
	return container, nil
}
`

const recorderContainer = `package diogen

import (
	amqp "gitlab.com/gorib/amqp"
	pry "gitlab.com/gorib/pry"
	scribus "gitlab.com/gorib/waffle/tools/scribus"
)

type Replacements struct {
	PryLogger       pry.Logger
	ScribusConsumer scribus.Consumer
	AmqpPublisher   amqp.Publisher
	PryLoggers      []pry.Logger
}

type Application struct{}

func InitAppWith(replacements Replacements) (*Application, error) {
	return container, nil
}
`

const narrowPublisherContainer = `package diogen

import (
	publisher "example.test/shop/pkg/publisher"
)

type Replacements struct {
	// gitlab.com/gorib/amqp.NewPublisher
	Publisher publisher.Publisher
}

type Application struct{}

func InitAppWith(replacements Replacements) (*Application, error) {
	return container, nil
}
`

const outboundContainer = `package diogen

import (
	pb "example.test/shop/api/pb"
)

type Replacements struct {
	// example.test/shop/api/pb.NewAccountServiceClient
	PbAccountServiceClient pb.AccountServiceClient
}

type Application struct{}

func InitAppWith(replacements Replacements) (*Application, error) {
	return container, nil
}
`

const commandsContainer = `package diogen

import (
	app "gitlab.com/gorib/waffle/app"
)

type Replacements struct{}

type Application struct {
	migrateCommand         app.Command
	appCommandAsCommandTag []app.Command
}

func InitAppWith(replacements Replacements) (*Application, error) {
	return container, nil
}
`

const cacheContainer = `package diogen

import (
	sql "gitlab.com/gorib/sql"
	redis "gitlab.com/gorib/redis"
	hilton "gitlab.com/gorib/hilton"
	pry "gitlab.com/gorib/pry"
)

type Replacements struct {
	SqlDb          sql.Db
	RedisDb        redis.Db
	HiltonProvider hilton.Provider
	PryLogger      pry.Logger
}

type Application struct{}

func InitAppWith(replacements Replacements) (*Application, error) {
	return container, nil
}
`

const paramsContainer = `package diogen

import (
	"context"

	di "example.test/shop/config/di"
	client "example.test/shop/client"
	pry "gitlab.com/gorib/pry"
)

type Limits struct{}

type Replacements struct {
	PryLogger pry.Logger
}

type Application struct{}

func InitAppWith(ctx context.Context, app *di.App, clientOpts *client.Options, limits Limits, onClose func(name string) error, replacements Replacements) (*Application, error) {
	return container, nil
}
`

const contextContainer = `package diogen

import "context"

type Replacements struct{}

type Application struct{}

func InitAppWith(ctx context.Context, replacements Replacements) (*Application, error) {
	return container, nil
}
`

const paramsHook = "package harness\n\n" +
	"func (a *App) params(ctx context.Context) (app *di.App, clientOpts *client.Options, limits diogen.Limits, onClose func(string) error, err error) {\n" +
	"\treturn nil, nil, diogen.Limits{}, nil, nil\n}\n"

func module(t *testing.T, container string, extra map[string]string) {
	t.Helper()
	t.Chdir(t.TempDir())

	files := map[string]string{
		"go.mod": "module example.test/shop\n\ngo 1.27\n",
		"config/di/di.go": "//go:generate diogen -output config/diogen/di_gen.go $GOFILE\n\n" +
			"package di\n",
		"config/diogen/di_gen.go": container,
	}
	for name, body := range extra {
		files[name] = body
	}
	for name, body := range files {
		place(t, name, body)
	}
}

func place(t *testing.T, name, body string) {
	t.Helper()
	path := filepath.FromSlash(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, args ...string) {
	t.Helper()
	if err := New().Run(t.Context(), args...); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func written(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.FromSlash(name))
	if err != nil {
		t.Fatalf("%s was not written: %v", name, err)
	}
	return string(body)
}

func generatedFile(t *testing.T) string {
	t.Helper()
	return written(t, "tests/internal/harness/harness_gen.go")
}

func contains(t *testing.T, name, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s is missing %q:\n%s", name, want, body)
		}
	}
}

func lacks(t *testing.T, name, body string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(body, bad) {
			t.Errorf("%s holds %q:\n%s", name, bad, body)
		}
	}
}

func TestFirstRunScaffoldsTheSuiteAndGeneratesTheHarness(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/order/cmd/migrate/main.go": "package migrate\n",
	})

	run(t)

	contains(t, "harness.go", written(t, "tests/internal/harness/harness.go"), "//go:generate go tool specrig harness")
	contains(t, "suite_test.go", written(t, "tests/suite_test.go"), "harness.Main(m)")
	contains(t, "boot_test.go", written(t, "tests/boot_test.go"), "harness.Boot(", "app.Serve()")
	contains(t, "migrations_test.go", written(t, "tests/migrations_test.go"), "harness.CheckMigrations(")
	contains(t, "replacements_test.go", written(t, "config/diogen/replacements_test.go"), "replacements.Check()")

	contains(t, "harness_gen.go", generatedFile(t),
		"// Code generated by specrig harness. DO NOT EDIT.",
		`sqltest.Env("SHOP_TEST_DSN")`,
		`sqltest.Database("shop_test")`,
		"sqltest.Ephemeral()",
		"orderMigrate.NewMigrations()",
		"resource.New(postgres",
		// Boot brings the resources up itself, so a scenario cannot forget.
		"if err := Up(ctx); err != nil {",
		"if cut.SqlDb == nil {\n\t\tcut.SqlDb, app.Postgres = postgres.Db(), postgres",
	)
}

// The loop belongs to the migrate command, so a harness that spells goose out
// again is one that can build a schema the daemon does not.
func TestMigrationsGoThroughTheCommandsOwnLoopInNameOrder(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/rent/cmd/migrate/main.go":    "package migrate\n",
		"domain/invoice/cmd/migrate/main.go": "package migrate\n",
	})

	run(t)

	body := generatedFile(t)
	lacks(t, "harness_gen.go", body, "goose.")
	if strings.Index(body, "invoiceMigrate.NewMigrations(),\n\trentMigrate") < 0 {
		t.Errorf("the sources are not in name order:\n%s", body)
	}
	contains(t, "harness_gen.go", body,
		"waffleMigrate.CheckShared(sources)",
		`"invoice": invoiceMigrate.NewMigrations()`,
		"sqltest.Suffix(name)",
	)
}

// Only the connection's label, and read off the container rather than off a
// naming convention for environment variables.
func TestDeadNamesTheClientsOnTheConnectionAndWhatItReads(t *testing.T) {
	module(t, grpcContainer, nil)

	run(t)

	contains(t, "harness_gen.go", generatedFile(t),
		"if cut.BaseGrpcClientConnInterfaceForContractProtoContractServiceClient == nil {",
		`grpcClient.Dead("contract.ContractServiceClient (DUNGEON_HOST, DUNGEON_PORT)")`,
	)
}

// A field per variable, typed as the read is, so a misspelt key or a wrong type
// in the env hook does not compile.
func TestVariablesHoldWhatAnUnreplacedConstructorReads(t *testing.T) {
	module(t, grpcContainer, nil)

	run(t)

	got, imports := generatedEnvironment(t)
	for key, typ := range map[string]string{
		"THING_URL": "string", "THING_PORT": "uint16", "THING_TTL": "time.Duration", "THING_CONFIG": "string", "THING_SHARDS": "[]int",
	} {
		if v, ok := got[key]; !ok || v.Type != typ || !v.Always {
			t.Errorf("Variables.%s is %q, always set %v; want an always set %s", key, v.Type, v.Always, typ)
		}
	}
	for key, want := range map[string]string{"THING_URL": `"thing-url.harness.invalid"`, "THING_CONFIG": `"null"`} {
		if def := render(got[key].Default); def != want {
			t.Errorf("Variables.%s defaults to %s, want %s", key, def, want)
		}
	}
	if imports["time"] != "time" {
		t.Errorf("time.Duration is a field type, and time is not imported: %v", imports)
	}
	contains(t, "harness_gen.go", generatedFile(t), `put("THING_SHARDS", list(v.THING_SHARDS))`)
	// Under a seam Boot fills, so the constructor never runs.
	for _, key := range []string{"DB_DSN", "DUNGEON_HOST"} {
		if _, ok := got[key]; ok {
			t.Errorf("Variables has %s", key)
		}
	}
}

func TestAContainerOfBuildClosuresReadsTheSameGraph(t *testing.T) {
	module(t, grpcClosureContainer, map[string]string{
		"domain/shop/cmd/grpc/service.go": orderHandler,
	})

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body,
		`grpcClient.Dead("contract.ContractServiceClient (DUNGEON_HOST, DUNGEON_PORT)")`,
		`put("THING_PORT", v.THING_PORT)`,
		"clients.Order = orderProto.NewOrderServiceClient(conn)",
		"a.GrpcCommandServiceHandlerAsServiceTag()",
	)
	lacks(t, "harness_gen.go", body, "\tDB_DSN ", "\tDUNGEON_HOST ")
}

// The fallback is the container's answer, so it is the field's default and is
// written only when the hook changed it: [] does not survive the environment.
func TestAFallbackIsSetOnlyWhenTheHookChangesIt(t *testing.T) {
	module(t, grpcContainer, nil)

	run(t)

	got, _ := generatedEnvironment(t)
	for key, want := range map[string]string{
		"LOGLEVEL": `"info"`,
		"POLL":     "5 * time.Second",
		"ORIGINS":  "[]string{}",
		// Read under a filled seam and under an unfilled one: the unfilled one keeps it.
		"SQL_DSN": `"x"`,
	} {
		v, ok := got[key]
		switch {
		case !ok:
			t.Errorf("Variables has no %s", key)
		case v.Always:
			t.Errorf("Env sets %s whatever the hook did", key)
		case render(v.Default) != want:
			t.Errorf("Variables.%s defaults to %s, want %s", key, render(v.Default), want)
		}
	}
	contains(t, "harness_gen.go", generatedFile(t),
		"if v.LOGLEVEL != d.LOGLEVEL {",
		"if !slices.Equal(v.ORIGINS, d.ORIGINS) {",
	)
	// A fallback naming a declaration of its own file cannot be copied out of it.
	if _, ok := got["LOCAL"]; ok {
		t.Error("Variables has LOCAL")
	}
}

// The way out for a key no code reads through env, and not a way around a field:
// a key that has one is refused.
func TestCustomTakesWhatHasNoFieldAndRefusesWhatHasOne(t *testing.T) {
	module(t, grpcContainer, nil)

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body,
		"Custom map[string]string",
		`"THING_URL": true`,
		"for key, value := range v.Custom {",
		"if typed[key] {",
	)
	if got := strings.Count(body, "\tCustom "); got != 1 {
		t.Errorf("Custom is declared %d times — a variable of that name took the field:\n%s", got, body)
	}
}

func TestAVariableOnlyTheModuleReadsIsAFieldToo(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/shop/adapter/client.go": "package adapter\n\nimport \"gitlab.com/gorib/env\"\n\n" +
			"var key = env.NeedValue[string](\"SHOP_KEY\")\n\nvar skip = env.NeedValue[string](\"THING_URL\")\n",
	})

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body, `"shop-key.harness.invalid"`)
	if got := strings.Count(body, "\tTHING_URL "); got != 1 {
		t.Errorf("THING_URL is declared %d times:\n%s", got, body)
	}
}

// An empty list would reach the environment as "", which gitlab.com/gorib/env
// reads back as one element no number parses.
func TestAListPlaceholderIsOneElementTheEnvPackageReadsBack(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/shop/adapter/lists.go": "package adapter\n\nimport (\n\t\"time\"\n\n\t\"gitlab.com/gorib/env\"\n)\n\n" +
			"var flags = env.NeedArray[bool](\"FLAGS\")\n\nvar backoff, _ = env.MustArray[time.Duration](\"BACKOFF\")\n",
	})

	run(t)

	got, _ := generatedEnvironment(t)
	readsBack[int](t, "THING_SHARDS", got["THING_SHARDS"])
	readsBack[bool](t, "FLAGS", got["FLAGS"])
	readsBack[time.Duration](t, "BACKOFF", got["BACKOFF"])
}

func TestAFallbackConvertedToATypeIsAFieldOfThatType(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/shop/adapter/fallbacks.go": "package adapter\n\nimport (\n\tclock \"time\"\n\n\t\"gitlab.com/gorib/env\"\n)\n\n" +
			"var port = env.Value(\"PORT\", uint16(8080))\n\n" +
			"var timeout = env.Value(\"TIMEOUT\", clock.Duration(0))\n\n" +
			"var ratio = env.Value(\"RATIO\", float32(0.5))\n\n" +
			"var ticks = env.Value(\"TICKS\", int(clock.Second/clock.Millisecond))\n\n" +
			"var wait = env.Value(\"WAIT\", 3*clock.Second)\n",
	})

	run(t)

	got, imports := generatedEnvironment(t)
	for key, typ := range map[string]string{
		"PORT": "uint16", "TIMEOUT": "clock.Duration", "RATIO": "float32", "TICKS": "int", "WAIT": "clock.Duration",
	} {
		if got[key].Type != typ {
			t.Errorf("Variables.%s is %q, want %s", key, got[key].Type, typ)
		}
	}
	if imports["clock"] != "time" {
		t.Errorf("clock.Duration is a field type, and clock is not imported: %v", imports)
	}
}

func TestOneReadWithoutAFallbackMakesTheVariableAlwaysSet(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/shop/adapter/required.go": "package adapter\n\nimport (\n\tstdtime \"time\"\n\n\t\"gitlab.com/gorib/env\"\n)\n\n" +
			"var level = env.NeedValue[string](\"LOGLEVEL\")\n\nvar poll = env.NeedValue[stdtime.Duration](\"POLL\")\n",
	})

	run(t)

	got, imports := generatedEnvironment(t)
	if v := got["LOGLEVEL"]; !v.Always || render(v.Default) != `"loglevel.harness.invalid"` {
		t.Errorf("Variables.LOGLEVEL defaults to %s, always set %v", render(v.Default), v.Always)
	}
	if v := got["POLL"]; !v.Always || v.Type != "stdtime.Duration" || imports["stdtime"] != "time" {
		t.Errorf("Variables.POLL is %q, always set %v, under the imports %v", v.Type, v.Always, imports)
	}
}

func TestAMustReadHoldsAPlaceholderLikeANeedRead(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/shop/adapter/secret.go": "package adapter\n\nimport \"gitlab.com/gorib/env\"\n\n" +
			"var secret, _ = env.MustValue[string](\"SHOP_SECRET\")\n\nvar limits, _ = env.MustJson[map[string]int](\"SHOP_LIMITS\")\n",
	})

	run(t)

	got, _ := generatedEnvironment(t)
	for key, want := range map[string]string{"SHOP_SECRET": `"shop-secret.harness.invalid"`, "SHOP_LIMITS": `"null"`} {
		if v := got[key]; v.Type != "string" || !v.Always || render(v.Default) != want {
			t.Errorf("Variables.%s is %q defaulting to %s, always set %v; want an always set string %s",
				key, v.Type, render(v.Default), v.Always, want)
		}
	}
}

func TestReadsOfOneKeyThatDisagreeOnTheTypeAreRefusedNamingBoth(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/shop/adapter/client.go": "package adapter\n\nimport \"gitlab.com/gorib/env\"\n\nvar url = env.NeedValue[int](\"THING_URL\")\n",
	})

	err := New().Run(t.Context())

	if err == nil {
		t.Fatal("THING_URL read as a string and as an int was accepted")
	}
	line := strings.Count(grpcContainer[:strings.Index(grpcContainer, `"THING_URL"`)], "\n") + 1
	for _, want := range []string{
		"THING_URL",
		fmt.Sprintf("%s:%d:", filepath.Join("config", "diogen", "di_gen.go"), line),
		filepath.Join("domain", "shop", "adapter", "client.go") + ":5:",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.FromSlash("tests/internal/harness/harness_gen.go")); err == nil {
		t.Error("a harness_gen.go was written with one of the two types")
	}
}

func TestAKeyOnlyAFilledSeamReadsStaysOutThoughTheMarkupSpellsIt(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"config/di/database.go": "package di\n\nimport \"gitlab.com/gorib/env\"\n\nvar dsn = env.NeedValue[string](\"DB_DSN\")\n",
	})

	run(t)

	got, _ := generatedEnvironment(t)
	if _, ok := got["DB_DSN"]; ok {
		t.Error("Variables has DB_DSN, which only the database Boot supplies reads")
	}
}

// A field of the generated Variables: its type, its default in variables(), and
// whether Env sets it whatever the env hook did.
type generatedVariable struct {
	Type    string
	Default ast.Expr
	Always  bool
}

func generatedEnvironment(t *testing.T) (map[string]generatedVariable, map[string]string) {
	t.Helper()
	name := filepath.FromSlash("tests/internal/harness/harness_gen.go")
	file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s does not parse: %v", name, err)
	}
	out := map[string]generatedVariable{}
	structure := structType(file, "Variables")
	if structure == nil {
		t.Fatalf("%s declares no Variables", name)
	}
	for _, field := range structure.Fields.List {
		for _, ident := range field.Names {
			if ident.Name != "Custom" {
				out[ident.Name] = generatedVariable{Type: types.ExprString(field.Type)}
			}
		}
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "variables":
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				literal, ok := n.(*ast.CompositeLit)
				if !ok || types.ExprString(literal.Type) != "Variables" {
					return true
				}
				for _, elt := range literal.Elts {
					pair := elt.(*ast.KeyValueExpr)
					v := out[types.ExprString(pair.Key)]
					v.Default = pair.Value
					out[types.ExprString(pair.Key)] = v
				}
				return false
			})
		case "Env":
			// Only its own statements: a put under an if is a conditional one.
			for _, stmt := range fn.Body.List {
				expr, ok := stmt.(*ast.ExprStmt)
				if !ok {
					continue
				}
				if call, ok := expr.X.(*ast.CallExpr); ok && types.ExprString(call.Fun) == "put" {
					key, _ := strconv.Unquote(types.ExprString(call.Args[0]))
					v := out[key]
					v.Always = true
					out[key] = v
				}
			}
		}
	}
	return out, waffle.ImportAliases(file)
}

// Written as Env writes it: fmt.Sprint of each element's value — a time.Duration
// prints as "0s", not as the "0" it is spelled — joined by commas.
func readsBack[T env.Environment](t *testing.T, key string, v generatedVariable) {
	t.Helper()
	literal, ok := v.Default.(*ast.CompositeLit)
	if !ok || len(literal.Elts) != 1 {
		t.Errorf("Variables.%s defaults to %q, want a list of one", key, render(v.Default))
		return
	}
	var parts []string
	for _, elt := range literal.Elts {
		held, err := types.Eval(token.NewFileSet(), nil, token.NoPos, render(elt))
		if err != nil || held.Value == nil {
			t.Fatalf("Variables.%s holds %s, which is not a constant: %v", key, render(elt), err)
		}
		value, typ := reflect.ValueOf(constant.Val(held.Value)), reflect.TypeFor[T]()
		if !value.CanConvert(typ) {
			t.Fatalf("Variables.%s holds %s, which is not a %s", key, render(elt), typ)
		}
		parts = append(parts, fmt.Sprint(value.Convert(typ).Interface()))
	}
	t.Setenv(key, strings.Join(parts, ","))
	if got, err := env.MustArray[T](key); err != nil || len(got) != 1 {
		t.Errorf("%s=%s reads back as %v: %v", key, os.Getenv(key), got, err)
	}
}

// A recorder the harness installs is not a seam the scenario has to be granted:
// tests/internal is outside the allow-list scan, and the handle comes back on
// App instead of going in through Replacements.
func TestRecordersComeBackOnApp(t *testing.T) {
	module(t, recorderContainer, nil)

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body,
		"Logger    *pry.LogRecorder",
		"Consumer  *scribus.ConsumerRecorder",
		"Publisher *amqp.PublisherRecorder",
		"cut.PryLogger, app.Logger = logger, logger",
		"cut.ScribusConsumer, app.Consumer = consumer, consumer",
		"cut.AmqpPublisher, app.Publisher = publisher, publisher",
	)
	// A collection of loggers is a di.Tags seam, and one recorder is not a list.
	lacks(t, "harness_gen.go", body, "cut.PryLoggers")
}

func TestAPublisherNarrowedByTheServiceIsRecordedToo(t *testing.T) {
	module(t, narrowPublisherContainer, nil)

	run(t)

	contains(t, "harness_gen.go", generatedFile(t),
		"cut.Publisher, app.Publisher = publisher, publisher",
		"cut.Publisher = trace.Publisher(cut.Publisher)",
	)
}

func TestAClientTheScenarioSuppliesIsTracedThroughItsOwnConstructor(t *testing.T) {
	module(t, outboundContainer, nil)

	run(t)

	contains(t, "harness_gen.go", generatedFile(t),
		`pbProto "example.test/shop/api/pb"`,
		"cut.PbAccountServiceClient = pbProto.NewAccountServiceClient(trace.Outbound(cut.PbAccountServiceClient))",
	)
}

func TestCommandsAreTracedAlsoThroughTheirCollection(t *testing.T) {
	module(t, commandsContainer, nil)

	run(t)

	contains(t, "harness_gen.go", generatedFile(t),
		"return trace.Command(a.Application.MigrateCommand())",
		"func (a *App) AppCommandAsCommandTag() []waffleApp.Command {",
		"traced[i] = trace.Command(c)",
	)
}

func TestRedisBesideTheDatabaseAndTelemetryRecorded(t *testing.T) {
	module(t, cacheContainer, nil)

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body,
		`redistest.Env("SHOP_TEST_REDIS")`,
		`redistest.Docker("redis:8-alpine")`,
		"resource.New(postgres, cache)",
		"cut.RedisDb, app.Redis = cache.Db(), cache",
		"Telemetry *hilton.Recording",
		"cut.HiltonProvider, app.Telemetry = telemetry, telemetry",
	)
	lacks(t, "harness_gen.go", body, "= hilton.Noop()")
}

func TestRedisIsLeftOutWhenTheContainerHasNone(t *testing.T) {
	module(t, grpcContainer, nil)

	run(t)

	lacks(t, "harness_gen.go", generatedFile(t), "redistest", "cache")
}

func TestAGrpcServiceGetsATypedClientPerRegisteredHandler(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"domain/shop/cmd/grpc/service.go": orderHandler,
	})

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body,
		`orderProto "example.test/api/order"`,
		"Order orderProto.OrderServiceClient",
		"clients.Order = orderProto.NewOrderServiceClient(conn)",
		"waffleGrpc.Serve(",
		"a.GrpcCommandServiceHandlerAsServiceTag()",
		"a.ServerInterceptorAsMiddlewareTag()",
		"a.ServerStreamInterceptorAsStreamMiddlewareTag()",
	)
	// A member by name means the list was copied, and a tagged registration
	// added later would reach the daemon and not the suite.
	lacks(t, "harness_gen.go", body, "a.InterceptorLogMWAsServerInterceptor()", "waffleHttp")
}

func TestServicesOfOneNameArePrefixedByTheirPackage(t *testing.T) {
	clients := []client{
		{Path: "example.test/api/notification", Service: "TemplateService"},
		{Path: "example.test/api/document", Service: "TemplateService"},
		{Path: "example.test/api/document", Service: "DocumentService"},
	}

	name(clients)

	got := map[string]string{}
	for _, c := range clients {
		got[c.Field] = c.Alias
	}
	want := map[string]string{
		"NotificationTemplate": "notificationProto",
		"DocumentTemplate":     "documentProto",
		"Document":             "documentProto",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields %v, want %v", got, want)
	}
}

// Two of the nine services in sherpa serve both, and one Clients has to carry
// both transports.
func TestBothTransportsShareOneClients(t *testing.T) {
	module(t, bothContainer, nil)

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body,
		"waffleGrpc.Serve(",
		"waffleHttp.Serve(",
		"a.HttpServerMux()",
		"a.HttpCommandGlobal()",
		"a.HttpServerRouterAsRouterTag()",
	)
	if got := strings.Count(body, "type Clients struct"); got != 1 {
		t.Errorf("Clients is declared %d times:\n%s", got, body)
	}
}

// A gateway: no database at all, and the caller reaches it over HTTP.
func TestAGatewayGetsAnEmptyResourceSetAndAnHttpServer(t *testing.T) {
	module(t, httpContainer, nil)

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body, "resource.New()", "waffleHttp.Serve(")
	lacks(t, "harness_gen.go", body, "sqltest", "postgres", "cut.SqlDb", "waffleGrpc", "CheckMigrations")
	if _, err := os.Stat(filepath.FromSlash("tests/migrations_test.go")); err == nil {
		t.Error("a service with no database got a migrations test")
	}
}

// Nothing here compiles what it generates, so the call is read against the real
// signature instead.
func TestTheServeCallFitsTheHelper(t *testing.T) {
	module(t, httpContainer, nil)

	run(t)

	call, bound := serveCall(t, "tests/internal/harness/harness_gen.go", "waffleHttp")
	signature := reflect.TypeOf(waffleHttp.Serve)
	if len(call.Args) != signature.NumIn() {
		t.Errorf("Serve is passed %d arguments, and takes %d", len(call.Args), signature.NumIn())
	}
	if bound != signature.NumOut() {
		t.Errorf("Serve's results are bound to %d names, and it returns %d", bound, signature.NumOut())
	}
}

func serveCall(t *testing.T, name, pkg string) (call *ast.CallExpr, bound int) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.FromSlash(name), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s does not parse: %v", name, err)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		found, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := found.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Serve" {
			return true
		}
		if x, ok := selector.X.(*ast.Ident); !ok || x.Name != pkg {
			return true
		}
		call, bound = found, len(assign.Lhs)
		return false
	})
	if call == nil {
		t.Fatalf("%s has no %s.Serve call", name, pkg)
	}
	return call, bound
}

// The alternative is silent: no router recognised reads as a service serving
// no HTTP at all, and the suite loses its client.
func TestAContainerWiredToASingleRouterIsRefused(t *testing.T) {
	module(t, singleRouterContainer, nil)

	err := New().Run(t.Context())

	if err == nil {
		t.Fatal("a container that predates the collection was accepted")
	}
	for _, want := range []string{"NewHttpCommand", "RouterTag"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}

func TestAContainerWithNoGlobalChainIsRefused(t *testing.T) {
	module(t, noGlobalContainer, nil)

	err := New().Run(t.Context())

	if err == nil {
		t.Fatal("a container that keeps the chain in its routers was accepted")
	}
	if !strings.Contains(err.Error(), "http.Global") {
		t.Errorf("the error does not name the seam: %v", err)
	}
}

func TestHooksAreCalledWhenThePackageDeclaresThem(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"tests/internal/harness/harness.go": "package harness\n\n" +
			"func env(v *Variables) {}\n" +
			"func setup() error { return nil }\n" +
			"func resources() []resource.Resource { return nil }\n" +
			"type extra struct{ Drivers any }\n" +
			"func (a *App) boot(ctx context.Context, cut *diogen.Replacements) error { return nil }\n",
	})

	run(t)

	contains(t, "harness_gen.go", generatedFile(t),
		"env(&v)",
		"if err := setup(); err != nil {",
		"append([]resource.Resource{postgres}, resources()...)...",
		"\textra\n",
		"if err := app.boot(ctx, &cut); err != nil {",
	)
}

// Serve exists only for a container that has a transport to serve.
func TestTheBootTestServesOnlyWhatTheContainerHas(t *testing.T) {
	module(t, cacheContainer, nil)

	run(t)

	body := written(t, "tests/boot_test.go")
	contains(t, "boot_test.go", body, "harness.Boot(ctx, diogen.Replacements{})")
	lacks(t, "boot_test.go", body, "Serve()")
	lacks(t, "harness_gen.go", generatedFile(t), "func (a *App) Serve()")
}

// The context is Boot's own, so a container that takes nothing else needs no hook.
func TestTheContextParameterIsBootsOwn(t *testing.T) {
	module(t, contextContainer, nil)

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body, "diogen.InitAppWith(ctx, cut)")
	lacks(t, "harness_gen.go", body, "app.params(")
}

// In the container's order, and a name Boot uses itself or a package it imports
// is suffixed. The types are the hook's to spell, so their packages are not
// imported here: they would be unused.
func TestParametersComeFromTheParamsHook(t *testing.T) {
	module(t, paramsContainer, map[string]string{"tests/internal/harness/harness.go": paramsHook})

	run(t)

	body := generatedFile(t)
	contains(t, "harness_gen.go", body,
		"appParam, clientOpts, limits, onClose, err := app.params(ctx)\n\tif err != nil {\n\t\treturn nil, err\n\t}",
		"diogen.InitAppWith(ctx, appParam, clientOpts, limits, onClose, cut)",
	)
	lacks(t, "harness_gen.go", body, `"example.test/shop/client"`, `"example.test/shop/config/di"`)
}

func TestAMissingParamsHookIsRefusedWithItsSignature(t *testing.T) {
	module(t, paramsContainer, nil)

	err := New().Run(t.Context())

	if err == nil {
		t.Fatal("a container with parameters was accepted without the hook that supplies them")
	}
	for _, want := range []string{
		"func (a *App) params(ctx context.Context) (app *di.App, clientOpts *client.Options, limits diogen.Limits, onClose func(name string) error, err error)",
		`client "example.test/shop/client"`,
		`diogen "example.test/shop/config/diogen"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not hold %q: %v", want, err)
		}
	}
	// The hook goes into harness.go, and a harness.go that exists stops the scaffold.
	contains(t, "boot_test.go", written(t, "tests/boot_test.go"), "harness.Boot(")
	if _, err := os.Stat(filepath.FromSlash("tests/internal/harness/harness_gen.go")); err == nil {
		t.Error("a harness_gen.go that cannot compile was written")
	}
}

func TestNoHookIsCalledWhenNoneIsDeclared(t *testing.T) {
	module(t, grpcContainer, nil)

	run(t)

	lacks(t, "harness_gen.go", generatedFile(t), "env(&v)", "setup()", "resources()", "extra", "app.boot(")
}

// A scaffold deleted on purpose is not written back on every go generate, and
// an edited one is not overwritten; the generated file always is.
func TestALaterRunRewritesOnlyTheGeneratedFile(t *testing.T) {
	module(t, grpcContainer, nil)
	run(t)
	place(t, "tests/boot_test.go", "package tests // mine\n")
	if err := os.Remove(filepath.FromSlash("tests/suite_test.go")); err != nil {
		t.Fatal(err)
	}
	place(t, "config/diogen/di_gen.go", cacheContainer)

	run(t)

	contains(t, "boot_test.go", written(t, "tests/boot_test.go"), "// mine")
	if _, err := os.Stat(filepath.FromSlash("tests/suite_test.go")); err == nil {
		t.Error("a deleted scaffold was written back")
	}
	contains(t, "harness_gen.go", generatedFile(t), "resource.New(postgres, cache)")
}

// go generate runs the directive from the harness package, not from the root.
func TestARunFromTheHarnessPackageWritesAtTheRoot(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"tests/internal/harness/harness.go": "package harness\n",
	})
	t.Chdir(filepath.FromSlash("tests/internal/harness"))
	beside, err := filepath.Abs(generated)
	if err != nil {
		t.Fatal(err)
	}

	run(t)

	if _, err := os.Stat(beside); err != nil {
		t.Fatalf("the generated file is not beside harness.go: %v", err)
	}
}

func TestAHandWrittenHarnessIsRefusedByName(t *testing.T) {
	module(t, grpcContainer, map[string]string{
		"tests/internal/harness/resources.go": "package harness\n\nfunc Up() {}\n\nvar postgres = 1\n",
	})

	err := New().Run(t.Context())

	if err == nil {
		t.Fatal("a package declaring Up beside the generated one was accepted")
	}
	for _, want := range []string{"resources.go", "Up", "postgres", "hooks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.FromSlash("tests/internal/harness/harness_gen.go")); err == nil {
		t.Error("the generated file was written beside a package it cannot compile with")
	}
}

func TestAnOperandIsRefused(t *testing.T) {
	module(t, grpcContainer, nil)

	err := New().Run(t.Context(), "init")

	if err == nil || !strings.Contains(err.Error(), "init") {
		t.Fatalf("run: %v", err)
	}
}
