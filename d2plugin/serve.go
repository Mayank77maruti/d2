package d2plugin

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "sync"

    "github.com/spf13/pflag"

    "oss.terrastruct.com/d2/d2graph"
    "oss.terrastruct.com/util-go/xmain"
)

// Serve returns a xmain.RunFunc that will invoke the plugin p as necessary to service the
// calling d2 CLI.
//
// See implementation of d2plugin-dagre in the ./cmd directory.
//
// Also see execPlugin in exec.go for the d2 binary plugin protocol.
func Serve(p Plugin) xmain.RunFunc {
    return func(ctx context.Context, ms *xmain.State) (err error) {
        // Introduce a mutex to serialize writes to ms.Stdout.
        var mu sync.Mutex
        writeOutput := func(b []byte) error {
            mu.Lock()
            defer mu.Unlock()
            _, err := ms.Stdout.Write(b)
            return err
        }

        if !ms.Opts.Flags.Parsed() {
            fs, err := p.Flags(ctx)
            if err != nil {
                return err
            }
            for _, f := range fs {
                f.AddToOpts(ms.Opts)
            }
            err = ms.Opts.Flags.Parse(ms.Opts.Args)
            if !errors.Is(err, pflag.ErrHelp) && err != nil {
                return xmain.UsageErrorf("failed to parse flags: %v", err)
            }
            if errors.Is(err, pflag.ErrHelp) {
                // At some point we want to write a friendly help.
                return info(ctx, p, ms, writeOutput)
            }
        }

        if len(ms.Opts.Flags.Args()) < 1 {
            return xmain.UsageErrorf("expected first argument to be subcmd name")
        }

        err = HydratePluginOpts(ctx, ms, p)
        if err != nil {
            return err
        }

        subcmd := ms.Opts.Flags.Arg(0)
        switch subcmd {
        case "info":
            return info(ctx, p, ms, writeOutput)
		case "flags":
			return flags(ctx, p, ms, writeOutput)
        case "layout":
            return layout(ctx, p, ms, writeOutput)
        case "postprocess":
            return postProcess(ctx, p, ms, writeOutput)
        case "routeedges":
            routingPlugin, ok := p.(RoutingPlugin)
            if !ok {
                return fmt.Errorf("plugin has routing feature but does not implement RoutingPlugin")
            }
            return routeEdges(ctx, routingPlugin, ms, writeOutput)
        default:
            return xmain.UsageErrorf("unrecognized command: %s", subcmd)
        }
    }
}

func info(ctx context.Context, p Plugin, ms *xmain.State, writeOutput func([]byte) error) error {
    info, err := p.Info(ctx)
    if err != nil {
        return err
    }
    b, err := json.Marshal(info)
    if err != nil {
        return err
    }
    return writeOutput(b)
}

func flags(ctx context.Context, p Plugin, ms *xmain.State, writeOutput func([]byte) error) error {
    flags, err := p.Flags(ctx)
    if err != nil {
        return err
    }
    b, err := json.Marshal(flags)
    if err != nil {
        return err
    }
    return writeOutput(b)
}

func layout(ctx context.Context, p Plugin, ms *xmain.State, writeOutput func([]byte) error) error {
    in, err := io.ReadAll(ms.Stdin)
    if err != nil {
        return err
    }
    var g d2graph.Graph
    if err := d2graph.DeserializeGraph(in, &g); err != nil {
        return fmt.Errorf("failed to unmarshal input to graph: %s", in)
    }
    err = p.Layout(ctx, &g)
    if err != nil {
        return err
    }
    b, err := d2graph.SerializeGraph(&g)
    if err != nil {
        return err
    }
    return writeOutput(b)
}

func postProcess(ctx context.Context, p Plugin, ms *xmain.State, writeOutput func([]byte) error) error {
    in, err := io.ReadAll(ms.Stdin)
    if err != nil {
        return err
    }

    out, err := p.PostProcess(ctx, in)
    if err != nil {
        return err
    }

    return writeOutput(out)
}

func routeEdges(ctx context.Context, p RoutingPlugin, ms *xmain.State, writeOutput func([]byte) error) error {
    inRaw, err := io.ReadAll(ms.Stdin)
    if err != nil {
        return err
    }

    var in routeEdgesInput
    err = json.Unmarshal(inRaw, &in)
    if err != nil {
        return err
    }

    var g d2graph.Graph
    if err := d2graph.DeserializeGraph(in.G, &g); err != nil {
        return fmt.Errorf("failed to unmarshal input graph to graph: %s", in)
    }

    var gedges d2graph.Graph
    if err := d2graph.DeserializeGraph(in.GEdges, &gedges); err != nil {
        return fmt.Errorf("failed to unmarshal input edges graph to graph: %s", in)
    }

    err = p.RouteEdges(ctx, &g, gedges.Edges)
    if err != nil {
        return err
    }

    b, err := d2graph.SerializeGraph(&g)
    if err != nil {
        return err
    }
    return writeOutput(b)
}
