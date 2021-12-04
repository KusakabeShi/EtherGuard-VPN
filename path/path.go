package path

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	orderedmap "github.com/KusakabeSi/EtherGuard-VPN/orderdmap"
	yaml "gopkg.in/yaml.v2"
)

const Infinity = float64(99999)

func (g *IG) GetCurrentTime() time.Time {
	return time.Now().Add(g.ntp_offset).Round(0)
}

type Latency struct {
	ping           float64
	ping_old       float64
	additionalCost float64
	validUntil     time.Time
}

type Fullroute struct {
	Next mtypes.NextHopTable `yaml:"NextHopTable"`
	Dist mtypes.DistTable    `yaml:"DistanceTable"`
}

// IG is a graph of integers that satisfies the Graph interface.
type IG struct {
	Vert                      map[mtypes.Vertex]bool
	edges                     map[mtypes.Vertex]map[mtypes.Vertex]*Latency
	edgelock                  *sync.RWMutex
	StaticMode                bool
	JitterTolerance           float64
	JitterToleranceMultiplier float64
	SuperNodeInfoTimeout      time.Duration
	RecalculateCoolDown       time.Duration
	TimeoutCheckInterval      time.Duration
	recalculateTime           time.Time
	dlTable                   mtypes.DistTable
	nhTable                   mtypes.NextHopTable
	changed                   bool
	NhTableExpire             time.Time
	IsSuperMode               bool
	loglevel                  mtypes.LoggerInfo

	ntp_wg      sync.WaitGroup
	ntp_info    mtypes.NTPInfo
	ntp_offset  time.Duration
	ntp_servers orderedmap.OrderedMap // serverurl:lentancy
}

func NewGraph(num_node int, IsSuperMode bool, theconfig mtypes.GraphRecalculateSetting, ntpinfo mtypes.NTPInfo, loglevel mtypes.LoggerInfo) *IG {
	g := IG{
		edgelock:                  &sync.RWMutex{},
		StaticMode:                theconfig.StaticMode,
		JitterTolerance:           theconfig.JitterTolerance,
		JitterToleranceMultiplier: theconfig.JitterToleranceMultiplier,
		RecalculateCoolDown:       mtypes.S2TD(theconfig.RecalculateCoolDown),
		TimeoutCheckInterval:      mtypes.S2TD(theconfig.TimeoutCheckInterval),
		ntp_info:                  ntpinfo,
	}
	g.Vert = make(map[mtypes.Vertex]bool, num_node)
	g.edges = make(map[mtypes.Vertex]map[mtypes.Vertex]*Latency, num_node)
	g.IsSuperMode = IsSuperMode
	g.loglevel = loglevel
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

func (g *IG) ShouldUpdate(u mtypes.Vertex, v mtypes.Vertex, newval float64) bool {
	oldval := math.Abs(g.OldWeight(u, v, false) * 1000)
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
		return g.GetWeightType(oldval) != g.GetWeightType(newval)
	}
}

func (g *IG) CheckAnyShouldUpdate() bool {
	vert := g.Vertices()
	for u, _ := range vert {
		for v, _ := range vert {
			if u != v {
				newVal := g.Weight(u, v, false)
				if g.ShouldUpdate(u, v, newVal) {
					return true
				}
			}
		}
	}
	return false
}

func (g *IG) RecalculateNhTable(checkchange bool) (changed bool) {
	if g.StaticMode {
		if g.changed {
			changed = checkchange
		}
		return
	}
	if !g.CheckAnyShouldUpdate() {
		return
	}
	if g.recalculateTime.Add(g.RecalculateCoolDown).Before(time.Now()) {
		dist, next, _ := g.FloydWarshall(false)
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
		g.recalculateTime = time.Now()
	}
	return
}

func (g *IG) RemoveVirt(v mtypes.Vertex, recalculate bool, checkchange bool) (changed bool) { //Waiting for test
	g.edgelock.Lock()
	delete(g.Vert, v)
	delete(g.edges, v)
	for u, _ := range g.edges {
		delete(g.edges[u], v)
	}
	g.edgelock.Unlock()
	g.changed = true
	if recalculate {
		changed = g.RecalculateNhTable(checkchange)
	}
	return
}

func (g *IG) UpdateLatency(src mtypes.Vertex, dst mtypes.Vertex, val float64, TimeToAlive float64, SuperAdditionalCost float64, recalculate bool, checkchange bool) (changed bool) {
	return g.UpdateLatencyMulti([]mtypes.PongMsg{{
		Src_nodeID:     src,
		Dst_nodeID:     dst,
		Timediff:       val,
		AdditionalCost: SuperAdditionalCost,
		TimeToAlive:    TimeToAlive,
	}}, recalculate, checkchange)
}

func (g *IG) UpdateLatencyMulti(pong_info []mtypes.PongMsg, recalculate bool, checkchange bool) (changed bool) {

	g.edgelock.Lock()
	should_update := false
	for _, pong_msg := range pong_info {
		u := pong_msg.Src_nodeID
		v := pong_msg.Dst_nodeID
		w := pong_msg.Timediff
		additionalCost := pong_msg.AdditionalCost
		if additionalCost < 0 {
			additionalCost = 0
		}
		g.Vert[u] = true
		g.Vert[v] = true
		if _, ok := g.edges[u]; !ok {
			g.recalculateTime = time.Time{}
			g.edges[u] = make(map[mtypes.Vertex]*Latency)
		}
		g.edgelock.Unlock()
		should_update = should_update || g.ShouldUpdate(u, v, w)
		g.edgelock.Lock()

		if _, ok := g.edges[u][v]; ok {
			g.edges[u][v].ping = w
			g.edges[u][v].validUntil = time.Now().Add(mtypes.S2TD(pong_msg.TimeToAlive))
			g.edges[u][v].additionalCost = additionalCost / 1000
		} else {
			g.edges[u][v] = &Latency{
				ping:           w,
				ping_old:       Infinity,
				validUntil:     time.Now().Add(mtypes.S2TD(pong_msg.TimeToAlive)),
				additionalCost: additionalCost / 1000,
			}
		}
	}
	g.edgelock.Unlock()
	if should_update && recalculate {
		changed = g.RecalculateNhTable(checkchange)
	}
	return
}
func (g *IG) Vertices() map[mtypes.Vertex]bool {
	vr := make(map[mtypes.Vertex]bool)
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	for k, v := range g.Vert { //copy a new list
		vr[k] = v
	}
	return vr
}
func (g IG) Neighbors(v mtypes.Vertex) (vs []mtypes.Vertex) {
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	for k := range g.edges[v] { //copy a new list
		vs = append(vs, k)
	}
	return vs
}

func (g *IG) Next(u, v mtypes.Vertex) *mtypes.Vertex {
	if _, ok := g.nhTable[u]; !ok {
		return nil
	}
	if _, ok := g.nhTable[u][v]; !ok {
		return nil
	}
	return g.nhTable[u][v]
}

func (g *IG) Weight(u, v mtypes.Vertex, withAC bool) (ret float64) {
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	//defer func() { fmt.Println(u, v, ret) }()
	if u == v {
		return 0
	}
	if _, ok := g.edges[u]; !ok {
		return Infinity
	}
	if _, ok := g.edges[u][v]; !ok {
		return Infinity
	}
	if time.Now().After(g.edges[u][v].validUntil) {
		return Infinity
	}
	ret = g.edges[u][v].ping
	if withAC {
		ret += g.edges[u][v].additionalCost
	}
	if ret >= Infinity {
		return Infinity
	}
	return
}

func (g *IG) OldWeight(u, v mtypes.Vertex, withAC bool) (ret float64) {
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	if u == v {
		return 0
	}
	if _, ok := g.edges[u]; !ok {
		return Infinity
	}
	if _, ok := g.edges[u][v]; !ok {
		return Infinity
	}
	ret = g.edges[u][v].ping_old
	if withAC {
		ret += g.edges[u][v].additionalCost
	}
	if ret >= Infinity {
		return Infinity
	}
	return
}

func (g *IG) SetWeight(u, v mtypes.Vertex, weight float64) {
	g.edgelock.Lock()
	defer g.edgelock.Unlock()
	if _, ok := g.edges[u]; !ok {
		return
	}
	if _, ok := g.edges[u][v]; !ok {
		return
	}
	g.edges[u][v].ping = weight
}

func (g *IG) SetOldWeight(u, v mtypes.Vertex, weight float64) {
	g.edgelock.Lock()
	defer g.edgelock.Unlock()
	if _, ok := g.edges[u]; !ok {
		return
	}
	if _, ok := g.edges[u][v]; !ok {
		return
	}
	g.edges[u][v].ping_old = weight
}

func (g *IG) RemoveAllNegativeValue() {
	vert := g.Vertices()
	for u, _ := range vert {
		for v, _ := range vert {
			if g.Weight(u, v, true) < 0 {
				if g.loglevel.LogInternal {
					fmt.Printf("Internal: Remove negative value : edge[%v][%v] = 0\n", u, v)
				}
				g.SetWeight(u, v, 0)
			}
		}
	}
}

func (g *IG) FloydWarshall(again bool) (dist mtypes.DistTable, next mtypes.NextHopTable, err error) {
	if g.loglevel.LogInternal {
		if !again {
			fmt.Println("Internal: Start Floyd Warshall algorithm")
		} else {
			fmt.Println("Internal: Start Floyd Warshall algorithm again")

		}
	}
	vert := g.Vertices()
	dist = make(mtypes.DistTable)
	next = make(mtypes.NextHopTable)
	for u, _ := range vert {
		dist[u] = make(map[mtypes.Vertex]float64)
		next[u] = make(map[mtypes.Vertex]*mtypes.Vertex)
		for v, _ := range vert {
			dist[u][v] = Infinity
		}
		dist[u][u] = 0
		for _, v := range g.Neighbors(u) {
			w := g.Weight(u, v, true)
			wo := g.Weight(u, v, false)
			if w < Infinity {
				v := v
				dist[u][v] = w
				next[u][v] = &v
			}
			g.SetOldWeight(u, v, wo)
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
	for i := range dist {
		if dist[i][i] < 0 {
			if !again {
				if g.loglevel.LogInternal {
					fmt.Println("Internal: Error: Negative cycle detected")
				}
				g.RemoveAllNegativeValue()
				err = errors.New("negative cycle detected")
				dist, next, _ = g.FloydWarshall(true)
				return
			} else {
				dist = make(mtypes.DistTable)
				next = make(mtypes.NextHopTable)
				err = errors.New("negative cycle detected again!")
				if g.loglevel.LogInternal {
					fmt.Println("Internal: Error: Negative cycle detected again")
				}
				return
			}
		}
	}
	return
}

func Path(u, v mtypes.Vertex, next mtypes.NextHopTable) (path []mtypes.Vertex) {
	if next[u][v] == nil {
		return []mtypes.Vertex{}
	}
	path = []mtypes.Vertex{u}
	for u != v {
		u = *next[u][v]
		path = append(path, u)
	}
	return path
}

func (g *IG) SetNHTable(nh mtypes.NextHopTable) { // set nhTable from supernode
	g.nhTable = nh
	g.changed = true
	g.NhTableExpire = time.Now().Add(g.SuperNodeInfoTimeout)
}

func (g *IG) GetNHTable(recalculate bool) mtypes.NextHopTable {
	if recalculate && time.Now().After(g.NhTableExpire) {
		g.RecalculateNhTable(false)
	}
	return g.nhTable
}

func (g *IG) GetDtst() mtypes.DistTable {
	return g.dlTable
}

func (g *IG) GetEdges(isOld bool, withAC bool) (edges map[mtypes.Vertex]map[mtypes.Vertex]float64) {
	vert := g.Vertices()
	edges = make(map[mtypes.Vertex]map[mtypes.Vertex]float64, len(vert))
	for src, _ := range vert {
		edges[src] = make(map[mtypes.Vertex]float64, len(vert))
		for dst, _ := range vert {
			if src != dst {
				if isOld {
					edges[src][dst] = g.OldWeight(src, dst, withAC)
				} else {
					edges[src][dst] = g.Weight(src, dst, withAC)
				}
			}
		}
	}
	return
}

func (g *IG) GetBoardcastList(id mtypes.Vertex) (tosend map[mtypes.Vertex]bool) {
	tosend = make(map[mtypes.Vertex]bool)
	for _, element := range g.nhTable[id] {
		tosend[*element] = true
	}
	return
}

func (g *IG) GetBoardcastThroughList(self_id mtypes.Vertex, in_id mtypes.Vertex, src_id mtypes.Vertex) (tosend map[mtypes.Vertex]bool) {
	tosend = make(map[mtypes.Vertex]bool)
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

func printExample() {
	fmt.Println(`X 1   2   3   4   5   6
1 0   0.5 Inf Inf Inf Inf
2 0.5 0   0.5 0.5 Inf Inf
3 Inf 0.5 0   0.5 0.5 Inf
4 Inf 0.5 0.5 0   Inf 0.5
5 Inf Inf 0.5 Inf 0   Inf
6 Inf Inf Inf 0.5 Inf 0`)
}

func a2n(s string) (ret float64) {
	if s == "Inf" {
		return Infinity
	}
	ret, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return
}

func a2v(s string) mtypes.Vertex {
	ret, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		panic(err)
	}
	return mtypes.Vertex(ret)
}

func Solve(filePath string, pe bool) error {
	if pe {
		printExample()
		return nil
	}

	g := NewGraph(3, false, mtypes.GraphRecalculateSetting{	}, mtypes.NTPInfo{}, mtypes.LoggerInfo{LogInternal: true})
	inputb, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	input := string(inputb)
	lines := strings.Split(input, "\n")
	verts := strings.Fields(lines[0])
	for _, line := range lines[1:] {
		element := strings.Fields(line)
		src := a2v(element[0])
		for index, sval := range element[1:] {
			val := a2n(sval)
			dst := a2v(verts[index+1])
			if src != dst && val != Infinity {
				g.UpdateLatency(src, dst, val, 99999, 0, false, false)
			}
		}
	}
	dist, next, err := g.FloydWarshall(false)
	if err != nil {
		fmt.Println("Error:", err)
	}

	rr, _ := yaml.Marshal(Fullroute{
		Dist: dist,
		Next: next,
	})
	fmt.Print(string(rr))

	fmt.Println("\nHuman readable:")
	fmt.Println("src\tdist\t\tpath")
	for _, U := range verts[1:] {
		u := a2v(U)
		for _, V := range verts[1:] {
			v := a2v(V)
			if u != v {
				fmt.Printf("%d -> %d\t%3f\t%s\n", u, v, dist[u][v], fmt.Sprint(Path(u, v, next)))
			}
		}
	}
	return nil
}
