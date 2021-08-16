package path

import (
	"fmt"
	"math"
	"time"

	yaml "gopkg.in/yaml.v2"
)

var (
	timeout = time.Second * 3
)

// A Graph is the interface implemented by graphs that
// this algorithm can run on.
type Graph interface {
	Vertices() map[Vertex]bool
	Neighbors(v Vertex) []Vertex
	Weight(u, v Vertex) float64
}

// Nonnegative integer ID of vertex
type Vertex uint32

const Infinity = 99999

var Boardcast = Vertex(math.MaxUint32)

type Latency struct {
	ping float64
	time time.Time
}

type DistTable map[Vertex]map[Vertex]float64
type NextHopTable map[Vertex]map[Vertex]*Vertex

type Fullroute struct {
	Dist DistTable    `json:"total distance"`
	Next NextHopTable `json:"next hop"`
}

// IG is a graph of integers that satisfies the Graph interface.
type IG struct {
	Vert  map[Vertex]bool
	Edges map[Vertex]map[Vertex]Latency
}

func (g *IG) Init(num_node int) error {
	g.Vert = make(map[Vertex]bool, num_node)
	g.Edges = make(map[Vertex]map[Vertex]Latency, num_node)
	return nil
}

func (g *IG) Edge(u, v Vertex, w float64) {
	g.Vert[u] = true
	g.Vert[v] = true
	if _, ok := g.Edges[u]; !ok {
		g.Edges[u] = make(map[Vertex]Latency)
	}
	g.Edges[u][v] = Latency{
		ping: w,
		time: time.Now(),
	}
}
func (g IG) Vertices() map[Vertex]bool { return g.Vert }
func (g IG) Neighbors(v Vertex) (vs []Vertex) {
	for k := range g.Edges[v] {
		vs = append(vs, k)
	}
	return vs
}
func (g IG) Weight(u, v Vertex) float64 {
	if time.Now().Sub(g.Edges[u][v].time) < timeout {
		return g.Edges[u][v].ping
	}
	return Infinity
}

func FloydWarshall(g Graph) (dist DistTable, next NextHopTable) {
	vert := g.Vertices()
	dist = make(DistTable)
	next = make(NextHopTable)
	for u, _ := range vert {
		dist[u] = make(map[Vertex]float64)
		next[u] = make(map[Vertex]*Vertex)
		for v, _ := range vert {
			dist[u][v] = Infinity
		}
		dist[u][u] = 0
		for _, v := range g.Neighbors(u) {
			w := g.Weight(u, v)
			if w < Infinity {
				v := v
				dist[u][v] = w
				next[u][v] = &v
			}
		}
	}
	for k, _ := range vert {
		for i, _ := range vert {
			for j, _ := range vert {
				if dist[i][k] < Infinity && dist[k][j] < Infinity {
					if dist[i][j] > dist[i][k]+dist[k][j] {
						dist[i][j] = dist[i][k] + dist[k][j]
						next[i][j] = next[i][k]
					}
				}
			}
		}
	}
	return dist, next
}

func Path(u, v Vertex, next NextHopTable) (path []Vertex) {
	if next[u][v] == nil {
		return []Vertex{}
	}
	path = []Vertex{u}
	for u != v {
		u = *next[u][v]
		path = append(path, u)
	}
	return path
}

func GetBoardcastList(id Vertex, nh NextHopTable) (tosend map[Vertex]bool) {
	tosend = make(map[Vertex]bool)
	for _, element := range nh[id] {
		tosend[*element] = true
	}
	return
}

func GetBoardcastThroughList(id Vertex, src Vertex, nh NextHopTable) (tosend map[Vertex]bool) {
	tosend = make(map[Vertex]bool)
	for check_id, _ := range GetBoardcastList(id, nh) {
		for _, path_node := range Path(src, check_id, nh) {
			if path_node == id {
				tosend[check_id] = true
				continue
			}
		}
	}
	return
}

func Solve() {
	var g IG
	g.Init(4)
	g.Edge(1, 2, 0.5)
	g.Edge(2, 1, 0.5)
	g.Edge(2, 3, 2)
	g.Edge(3, 2, 2)
	g.Edge(2, 4, 0.7)
	g.Edge(4, 2, 2)
	dist, next := FloydWarshall(g)
	fmt.Println("pair\tdist\tpath")
	for u, m := range dist {
		for v, d := range m {
			if u != v {
				fmt.Printf("%d -> %d\t%3f\t%s\n", u, v, d, fmt.Sprint(Path(u, v, next)))
			}
		}
	}
	fmt.Print("Finish")
	rr, _ := yaml.Marshal(Fullroute{
		Dist: dist,
		Next: next,
	})
	fmt.Print(string(rr))
}
