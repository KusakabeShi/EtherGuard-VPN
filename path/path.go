package path

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	orderedmap "github.com/KusakabeSi/EtherGuardVPN/orderdmap"
	yaml "gopkg.in/yaml.v2"
)

const Infinity = float64(99999)

func (g *IG) GetCurrentTime() time.Time {
	return time.Now().Add(g.ntp_offset).Round(0)
}

// A Graph is the interface implemented by graphs that
// this algorithm can run on.
type Graph interface {
	Vertices() map[config.Vertex]bool
	Neighbors(v config.Vertex) []config.Vertex
	Weight(u, v config.Vertex) float64
}

type Latency struct {
	ping float64
	time time.Time
}

type Fullroute struct {
	Dist config.DistTable    `json:"total distance"`
	Next config.NextHopTable `json:"next hop"`
}

// IG is a graph of integers that satisfies the Graph interface.
type IG struct {
	Vert                      map[config.Vertex]bool
	edges                     map[config.Vertex]map[config.Vertex]Latency
	edgelock                  *sync.RWMutex
	JitterTolerance           float64
	JitterToleranceMultiplier float64
	NodeReportTimeout         time.Duration
	SuperNodeInfoTimeout      time.Duration
	RecalculateCoolDown       time.Duration
	RecalculateTime           time.Time
	dlTable                   config.DistTable
	nhTable                   config.NextHopTable
	NhTableHash               [32]byte
	NhTableExpire             time.Time
	IsSuperMode               bool

	ntp_wg      sync.WaitGroup
	ntp_log     bool
	ntp_info    config.NTPinfo
	ntp_offset  time.Duration
	ntp_servers orderedmap.OrderedMap // serverurl:lentancy
}

func S2TD(secs float64) time.Duration {
	return time.Duration(secs * float64(time.Second))
}

func NewGraph(num_node int, IsSuperMode bool, theconfig config.GraphRecalculateSetting, ntpinfo config.NTPinfo, logntp bool) *IG {
	g := IG{
		edgelock:                  &sync.RWMutex{},
		JitterTolerance:           theconfig.JitterTolerance,
		JitterToleranceMultiplier: theconfig.JitterToleranceMultiplier,
		NodeReportTimeout:         S2TD(theconfig.NodeReportTimeout),
		RecalculateCoolDown:       S2TD(theconfig.RecalculateCoolDown),
		ntp_info:                  ntpinfo,
	}
	g.Vert = make(map[config.Vertex]bool, num_node)
	g.edges = make(map[config.Vertex]map[config.Vertex]Latency, num_node)
	g.IsSuperMode = IsSuperMode
	g.ntp_log = logntp
	g.InitNTP()
	return &g
}

func (g *IG) GetWeightType(x float64) (y float64) {
	x = math.Abs(x)
	y = x
	if g.JitterTolerance > 0.001 && g.JitterToleranceMultiplier > 1 {
		t := g.JitterTolerance
		r := g.JitterToleranceMultiplier
		y = math.Pow(math.Ceil(math.Pow(x/t, 1/r)), r) * t
	}
	return y
}

func (g *IG) ShouldUpdate(u config.Vertex, v config.Vertex, newval float64) bool {
	oldval := math.Abs(g.Weight(u, v) * 1000)
	newval = math.Abs(newval * 1000)
	if g.IsSuperMode {
		if g.JitterTolerance > 0.001 && g.JitterToleranceMultiplier >= 1 {
			diff := math.Abs(newval - oldval)
			x := math.Max(oldval, newval)
			t := g.JitterTolerance
			r := g.JitterToleranceMultiplier
			return diff > t+x*(r-1) // https://www.desmos.com/calculator/raoti16r5n
		}
		return oldval == newval
	} else {
		return g.GetWeightType(oldval) == g.GetWeightType(newval)
	}
}

func (g *IG) RecalculateNhTable(checkchange bool) (changed bool) {
	if g.RecalculateTime.Add(g.RecalculateCoolDown).Before(time.Now()) {
		dist, next := FloydWarshall(g)
		changed = false
		if checkchange {
		CheckLoop:
			for src, dsts := range next {
				for dst, old_next := range dsts {
					nexthop := g.Next(src, dst)
					if old_next != nexthop {
						changed = true
						break CheckLoop
					}
				}
			}
		}
		g.dlTable, g.nhTable = dist, next
		g.NhTableExpire = time.Now().Add(g.NodeReportTimeout)
		g.RecalculateTime = time.Now()
	}
	return
}

func (g *IG) UpdateLentancy(u, v config.Vertex, dt time.Duration, checkchange bool) (changed bool) {
	g.edgelock.Lock()
	g.Vert[u] = true
	g.Vert[v] = true
	w := float64(dt) / float64(time.Second)
	if _, ok := g.edges[u]; !ok {
		g.edges[u] = make(map[config.Vertex]Latency)
	}
	g.edges[u][v] = Latency{
		ping: w,
		time: time.Now(),
	}
	g.edgelock.Unlock()
	if g.ShouldUpdate(u, v, w) {
		changed = g.RecalculateNhTable(checkchange)
	}
	return
}
func (g IG) Vertices() map[config.Vertex]bool {
	vr := make(map[config.Vertex]bool)
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	for k, v := range g.Vert { //copy a new list
		vr[k] = v
	}
	return vr
}
func (g IG) Neighbors(v config.Vertex) (vs []config.Vertex) {
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	for k := range g.edges[v] { //copy a new list
		vs = append(vs, k)
	}
	return vs
}

func (g IG) Next(u, v config.Vertex) *config.Vertex {
	if _, ok := g.nhTable[u]; !ok {
		return nil
	}
	if _, ok := g.nhTable[u][v]; !ok {
		return nil
	}
	return g.nhTable[u][v]
}

func (g IG) Weight(u, v config.Vertex) float64 {
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	if _, ok := g.edges[u]; !ok {
		g.edgelock.RUnlock()
		g.edgelock.Lock()
		g.edges[u] = make(map[config.Vertex]Latency)
		g.edgelock.Unlock()
		g.edgelock.RLock()
		return Infinity
	}
	if _, ok := g.edges[u][v]; !ok {
		return Infinity
	}
	if time.Now().After(g.edges[u][v].time.Add(g.NodeReportTimeout)) {
		return Infinity
	}
	return g.edges[u][v].ping
}

func FloydWarshall(g Graph) (dist config.DistTable, next config.NextHopTable) {
	vert := g.Vertices()
	dist = make(config.DistTable)
	next = make(config.NextHopTable)
	for u, _ := range vert {
		dist[u] = make(map[config.Vertex]float64)
		next[u] = make(map[config.Vertex]*config.Vertex)
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

func Path(u, v config.Vertex, next config.NextHopTable) (path []config.Vertex) {
	if next[u][v] == nil {
		return []config.Vertex{}
	}
	path = []config.Vertex{u}
	for u != v {
		u = *next[u][v]
		path = append(path, u)
	}
	return path
}

func (g *IG) SetNHTable(nh config.NextHopTable, table_hash [32]byte) { // set nhTable from supernode
	g.nhTable = nh
	g.NhTableHash = table_hash
	g.NhTableExpire = time.Now().Add(g.SuperNodeInfoTimeout)
}

func (g *IG) GetNHTable(checkChange bool) config.NextHopTable {
	if time.Now().After(g.NhTableExpire) {
		g.RecalculateNhTable(checkChange)
	}
	return g.nhTable
}

func (g *IG) GetDtst() config.DistTable {
	return g.dlTable
}

func (g *IG) GetEdges() (edges map[config.Vertex]map[config.Vertex]float64) {
	vert := g.Vertices()
	edges = make(map[config.Vertex]map[config.Vertex]float64, len(vert))
	for src, _ := range vert {
		edges[src] = make(map[config.Vertex]float64, len(vert))
		for dst, _ := range vert {
			if src != dst {
				edges[src][dst] = g.Weight(src, dst)
			}
		}
	}
	return
}

func (g *IG) GetBoardcastList(id config.Vertex) (tosend map[config.Vertex]bool) {
	tosend = make(map[config.Vertex]bool)
	for _, element := range g.nhTable[id] {
		tosend[*element] = true
	}
	return
}

func (g *IG) GetBoardcastThroughList(self_id config.Vertex, in_id config.Vertex, src_id config.Vertex) (tosend map[config.Vertex]bool) {
	tosend = make(map[config.Vertex]bool)
	for check_id, _ := range g.GetBoardcastList(self_id) {
		for _, path_node := range Path(src_id, check_id, g.nhTable) {
			if path_node == self_id && check_id != in_id {
				tosend[check_id] = true
				continue
			}
		}
	}
	return
}

func Solve() {
	var g IG
	//g.Init()
	g.UpdateLentancy(1, 2, S2TD(0.5), false)
	g.UpdateLentancy(2, 1, S2TD(0.5), false)
	g.UpdateLentancy(2, 3, S2TD(2), false)
	g.UpdateLentancy(3, 2, S2TD(2), false)
	g.UpdateLentancy(2, 4, S2TD(0.7), false)
	g.UpdateLentancy(4, 2, S2TD(2), false)
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
