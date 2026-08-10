// Package topology loads a declarative YAML graph description into a graph.Graph. It
// realizes the hybrid model (spec §6.1): the YAML declares the topology while tool and
// router functions are Go code registered by name in a Registry, and agent nodes are
// backed by a graph.AgentRunner.
package topology

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/yoann/kern-orch/internal/graph"
)

// Registry maps the names referenced in YAML to Go implementations.
type Registry struct {
	tools   map[string]graph.ToolFunc
	routers map[string]graph.RouteFunc
	runner  graph.AgentRunner

	// childStep builds the hook a nested run reports through. Wiring, like the runner:
	// it decides how a node is built, not what the graph means.
	childStep func(nodeID, graphRef string) graph.StepFunc

	// approval is the wait function every approval node in this graph blocks on. Uniform
	// across nodes (unlike childStep) because it is bound to one run's mailbox, not to a
	// particular node's identity.
	approval graph.ApprovalFunc
}

// NewRegistry creates a registry; runner may be nil if the graph has no agent nodes.
func NewRegistry(runner graph.AgentRunner) *Registry {
	return &Registry{
		tools:   make(map[string]graph.ToolFunc),
		routers: make(map[string]graph.RouteFunc),
		runner:  runner,
	}
}

// Tool registers a tool function under name.
func (r *Registry) Tool(name string, fn graph.ToolFunc) *Registry {
	r.tools[name] = fn
	return r
}

// OnChildStep makes every subgraph node report its nested run through the hook build
// returns. Unset, nested runs stay invisible exactly as before.
func (r *Registry) OnChildStep(build func(nodeID, graphRef string) graph.StepFunc) *Registry {
	r.childStep = build
	return r
}

// Router registers a conditional routing function under name.
func (r *Registry) Router(name string, fn graph.RouteFunc) *Registry {
	r.routers[name] = fn
	return r
}

// OnApproval sets the wait function every approval node in the loaded graph blocks on.
// Unset, a graph with an approval node refuses to load (see build) rather than hang
// forever with nothing configured to answer it.
func (r *Registry) OnApproval(fn graph.ApprovalFunc) *Registry {
	r.approval = fn
	return r
}

type nodeSpec struct {
	ID     string `yaml:"id"`
	Type   string `yaml:"type"`
	Func   string `yaml:"func"`
	Skill  string `yaml:"skill"`
	Prompt string `yaml:"prompt"`
	Graph  string `yaml:"graph"` // subgraph nodes: path to the nested graph file
}

type edgeSpec struct {
	From   string   `yaml:"from"`
	To     []string `yaml:"to"`
	Router string   `yaml:"router"`
}

type spec struct {
	Entry string     `yaml:"entry"`
	Nodes []nodeSpec `yaml:"nodes"`
	Edges []edgeSpec `yaml:"edges"`
}

// subResolver turns a subgraph node's file reference into a built nested graph.
// subResolver resolves a subgraph reference to a built graph and the file it came from.
type subResolver func(nodeID, ref string) (*graph.Graph, string, error)

// Load parses YAML and builds a graph from inline definitions. Subgraph nodes are not
// supported here (they need file resolution) — use LoadFile for graphs with subgraphs.
func Load(data []byte, reg *Registry) (*graph.Graph, error) {
	noSub := func(nodeID, _ string) (*graph.Graph, string, error) {
		return nil, "", fmt.Errorf("topology: node %q is a subgraph; load via a file with LoadFile", nodeID)
	}
	return build(data, reg, noSub)
}

// build parses YAML and assembles the graph, delegating subgraph resolution to resolveSub.
func build(data []byte, reg *Registry, resolveSub subResolver) (*graph.Graph, error) {
	var sp spec
	if err := yaml.Unmarshal(data, &sp); err != nil {
		return nil, fmt.Errorf("topology: parse yaml: %w", err)
	}
	if sp.Entry == "" {
		return nil, fmt.Errorf("topology: no entry declared")
	}

	g := graph.NewGraph().SetEntry(sp.Entry)
	for _, n := range sp.Nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("topology: node with empty id")
		}
		switch n.Type {
		case "tool":
			fn, ok := reg.tools[n.Func]
			if !ok {
				return nil, fmt.Errorf("topology: node %q: unknown tool func %q", n.ID, n.Func)
			}
			g.AddNode(graph.NewToolNode(n.ID, fn))
		case "agent":
			if reg.runner == nil {
				return nil, fmt.Errorf("topology: node %q is an agent but no runner is configured", n.ID)
			}
			g.AddNode(graph.NewAgentNode(n.ID, n.Prompt, reg.runner))
		case "subgraph":
			if n.Graph == "" {
				return nil, fmt.Errorf("topology: subgraph node %q missing `graph` file reference", n.ID)
			}
			sub, ref, err := resolveSub(n.ID, n.Graph)
			if err != nil {
				return nil, err
			}
			opts := []graph.SubgraphOption{graph.WithGraphRef(ref)}
			if reg.childStep != nil {
				opts = append(opts, graph.WithChildStep(reg.childStep))
			}
			g.AddNode(graph.NewSubgraphNode(n.ID, sub, opts...))
		case "approval":
			if reg.approval == nil {
				return nil, fmt.Errorf("topology: node %q is an approval but no decision source is configured", n.ID)
			}
			g.AddNode(graph.NewApprovalNode(n.ID, reg.approval))
		default:
			return nil, fmt.Errorf("topology: node %q: invalid type %q (want tool|agent|subgraph|approval)", n.ID, n.Type)
		}
	}

	for _, e := range sp.Edges {
		switch {
		case len(e.To) > 0 && e.Router != "":
			return nil, fmt.Errorf("topology: edge from %q has both `to` and `router`", e.From)
		case e.Router != "":
			route, ok := reg.routers[e.Router]
			if !ok {
				return nil, fmt.Errorf("topology: edge from %q: unknown router %q", e.From, e.Router)
			}
			g.AddEdge(e.From, route)
		case len(e.To) > 0:
			g.AddEdge(e.From, graph.Static(e.To...))
		default:
			return nil, fmt.Errorf("topology: edge from %q has neither `to` nor `router`", e.From)
		}
	}

	if err := g.Validate(); err != nil {
		return nil, err
	}
	return g, nil
}
