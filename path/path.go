package path

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	orderedmap "github.com/KusakabeSi/EtherGuard-VPN/orderdmap"
	yaml "gopkg.in/yaml.v2"
)

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
	Vert                 map[mtypes.Vertex]bool
	edges                map[mtypes.Vertex]map[mtypes.Vertex]*Latency
	edgelock             *sync.RWMutex
	gsetting             mtypes.GraphRecalculateSetting
	SuperNodeInfoTimeout time.Duration
	RecalculateCoolDown  time.Duration
	TimeoutCheckInterval time.Duration
	recalculateTime      time.Time
	dlTable              mtypes.DistTable
	nhTable              mtypes.NextHopTable
	changed              bool
	NhTableExpire        time.Time
	IsSuperMode          bool
	loglevel             mtypes.LoggerInfo

	ntp_wg      sync.WaitGroup
	ntp_info    mtypes.NTPInfo
	ntp_init_t  time.Time
	ntp_offset  time.Duration
	ntp_servers orderedmap.OrderedMap // serverurl:lentancy
}

func NewGraph(num_node int, IsSuperMode bool, theconfig mtypes.GraphRecalculateSetting, ntpinfo mtypes.NTPInfo, loglevel mtypes.LoggerInfo) (*IG, error) {
	g := IG{
		edgelock:             &sync.RWMutex{},
		gsetting:             theconfig,
		RecalculateCoolDown:  mtypes.S2TD(theconfig.RecalculateCoolDown),
		TimeoutCheckInterval: mtypes.S2TD(theconfig.TimeoutCheckInterval),
		ntp_info:             ntpinfo,
	}
	g.Vert = make(map[mtypes.Vertex]bool, num_node)
	g.edges = make(map[mtypes.Vertex]map[mtypes.Vertex]*Latency, num_node)
	g.IsSuperMode = IsSuperMode
	g.loglevel = loglevel
	g.InitNTP()
	return &g, nil
}

func (g *IG) GetWeightType(x float64) (y float64) {
	x = math.Abs(x)
	y = x
	if g.gsetting.JitterTolerance > 0.001 && g.gsetting.JitterToleranceMultiplier > 1 {
		t := g.gsetting.JitterTolerance
		r := g.gsetting.JitterToleranceMultiplier
		y = math.Pow(math.Ceil(math.Pow(x/t, 1/r)), r) * t
	}
	return y
}

func (g *IG) ShouldUpdate(oldval float64, newval float64, withCooldown bool) bool {
	if (oldval >= mtypes.Infinity) != (newval >= mtypes.Infinity) {
		return true
	}
	if withCooldown {
		if g.recalculateTime.Add(g.RecalculateCoolDown).After(time.Now()) {
			return false
		}
	}
	oldval = math.Abs(oldval * 1000)
	newval = math.Abs(newval * 1000)
	if g.IsSuperMode {
		if g.gsetting.JitterTolerance > 0.001 && g.gsetting.JitterToleranceMultiplier >= 1 {
			diff := math.Abs(newval - oldval)
			x := math.Max(oldval, newval)
			t := g.gsetting.JitterTolerance
			r := g.gsetting.JitterToleranceMultiplier
			return diff > t+x*(r-1) // https://www.desmos.com/calculator/raoti16r5n
		}
		return oldval == newval
	} else {
		return g.GetWeightType(oldval) != g.GetWeightType(newval)
	}
}

func (g *IG) CheckAnyShouldUpdate(withCooldown bool) bool {
	vert := g.Vertices()
	for u := range vert {
		for v := range vert {
			if u != v {
				newVal := g.Weight(u, v, false)
				oldVal := g.OldWeight(u, v, false)
				if g.ShouldUpdate(oldVal, newVal, withCooldown) {
					return true
				}
			}
		}
	}
	return false
}

func (g *IG) RecalculateNhTable(checkchange bool) (changed bool) {
	if g.gsetting.StaticMode {
		if g.changed {
			changed = checkchange
		}
		return
	}
	if !g.CheckAnyShouldUpdate(true) {
		return
	}

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

	return
}

func (g *IG) RemoveVirt(v mtypes.Vertex, recalculate bool, checkchange bool) (changed bool) { //Waiting for test
	g.edgelock.Lock()
	delete(g.Vert, v)
	delete(g.edges, v)
	for u := range g.edges {
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
		newval := pong_msg.Timediff
		if dst_latency, ok := g.gsetting.ManualLatency[mtypes.NodeID_Broadcast]; ok {
			if _, ok := dst_latency[mtypes.NodeID_Broadcast]; ok {
				newval = dst_latency[mtypes.NodeID_Broadcast] / 1000 // s to ms
			}
			if _, ok := dst_latency[v]; ok {
				newval = dst_latency[v] / 1000 // s to ms
			}
		}
		if dst_latency, ok := g.gsetting.ManualLatency[u]; ok {
			if _, ok := dst_latency[mtypes.NodeID_Broadcast]; ok {
				newval = dst_latency[mtypes.NodeID_Broadcast] / 1000 // s to ms
			}
			if _, ok := dst_latency[v]; ok {
				newval = dst_latency[v] / 1000 // s to ms
			}
		}
		w := newval
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
		oldval := g.OldWeight(u, v, false)
		g.edgelock.Lock()
		should_update = should_update || g.ShouldUpdate(oldval, w, false)
		if _, ok := g.edges[u][v]; ok {
			g.edges[u][v].ping = w
			g.edges[u][v].validUntil = time.Now().Add(mtypes.S2TD(pong_msg.TimeToAlive))
			g.edges[u][v].additionalCost = additionalCost / 1000
		} else {
			g.edges[u][v] = &Latency{
				ping:           w,
				ping_old:       mtypes.Infinity,
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
func (g *IG) Neighbors(v mtypes.Vertex) (vs []mtypes.Vertex) {
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	for k := range g.edges[v] { //copy a new list
		vs = append(vs, k)
	}
	return vs
}

func (g *IG) Next(u, v mtypes.Vertex) mtypes.Vertex {
	if _, ok := g.nhTable[u]; !ok {
		return mtypes.NodeID_Invalid
	}
	if _, ok := g.nhTable[u][v]; !ok {
		return mtypes.NodeID_Invalid
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
		return mtypes.Infinity
	}
	if _, ok := g.edges[u][v]; !ok {
		return mtypes.Infinity
	}
	if time.Now().After(g.edges[u][v].validUntil) {
		return mtypes.Infinity
	}
	ret = g.edges[u][v].ping
	if withAC {
		ret += g.edges[u][v].additionalCost
	}
	if ret >= mtypes.Infinity {
		return mtypes.Infinity
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
		return mtypes.Infinity
	}
	if _, ok := g.edges[u][v]; !ok {
		return mtypes.Infinity
	}
	ret = g.edges[u][v].ping_old
	if withAC {
		ret += g.edges[u][v].additionalCost
	}
	if ret >= mtypes.Infinity {
		return mtypes.Infinity
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
	for u := range vert {
		for v := range vert {
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
	for u := range vert {
		dist[u] = make(map[mtypes.Vertex]float64)
		next[u] = make(map[mtypes.Vertex]mtypes.Vertex)
		for v := range vert {
			dist[u][v] = mtypes.Infinity
		}
		dist[u][u] = 0
		for _, v := range g.Neighbors(u) {
			w := g.Weight(u, v, true)
			wo := g.Weight(u, v, false)
			if w < mtypes.Infinity {
				v := v
				dist[u][v] = w
				next[u][v] = v
			}
			g.SetOldWeight(u, v, wo)
		}
	}
	for k := range vert {
		for i := range vert {
			for j := range vert {
				if dist[i][k] < mtypes.Infinity && dist[k][j] < mtypes.Infinity {
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
				err = errors.New("negative cycle detected again")
				if g.loglevel.LogInternal {
					fmt.Println("Internal: Error: Negative cycle detected again")
				}
				return
			}
		}
	}
	return
}

func (g *IG) Path(u, v mtypes.Vertex) (path []mtypes.Vertex, err error) {
	g.edgelock.RLock()
	defer g.edgelock.RUnlock()
	footprint := make(map[mtypes.Vertex]bool)
	for u != v {
		if _, has := footprint[u]; has {
			return path, fmt.Errorf("cycle detected in nhTable, s:%v e:%v path:%v", u, v, path)
		}
		if _, ok := g.nhTable[u]; !ok {
			return path, fmt.Errorf("nhTable[%v] not exist", u)
		}
		if _, ok := g.nhTable[u][v]; !ok {
			return path, fmt.Errorf("nhTable[%v][%v] not exist", u, v)
		}
		path = append(path, u)
		footprint[u] = true
		u = g.nhTable[u][v]
	}
	path = append(path, u)
	return path, nil
}

func (g *IG) SetNHTable(nh mtypes.NextHopTable) { // set nhTable from supernode
	g.edgelock.Lock()
	defer g.edgelock.Unlock()
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
	for src := range vert {
		edges[src] = make(map[mtypes.Vertex]float64, len(vert))
		for dst := range vert {
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
		tosend[element] = true
	}
	return
}

func (g *IG) GetBoardcastThroughList(self_id mtypes.Vertex, in_id mtypes.Vertex, src_id mtypes.Vertex) (tosend map[mtypes.Vertex]bool, errs []error) {
	tosend = make(map[mtypes.Vertex]bool)
	for check_id := range g.GetBoardcastList(self_id) {
		path, err := g.Path(src_id, check_id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, path_node := range path {
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

func ParseDistanceMatrix(input string) ([]mtypes.PongMsg, error) {
	lines := strings.Split(input, "\n")
	verts := strings.Fields(lines[0])
	ret := make([]mtypes.PongMsg, 0, len(verts)*len(verts))
	for li, line := range lines[1:] {
		element := strings.Fields(line)
		src, err := mtypes.String2NodeID(element[0])
		if err != nil {
			return ret, err
		}
		if len(element) != len(verts) {
			return ret, fmt.Errorf("parse error at line %v: element number mismatch to node id number", li)
		}
		for ei, sval := range element[1:] {
			val, err := mtypes.String2Float64(sval)
			if err != nil {
				return ret, err
			}
			dst, err := mtypes.String2NodeID(verts[ei+1])
			if err != nil {
				return ret, err
			}
			if src != dst && val != mtypes.Infinity {
				ret = append(ret, mtypes.PongMsg{
					Src_nodeID:  src,
					Dst_nodeID:  dst,
					Timediff:    val,
					TimeToAlive: 999999,
				})
			}
		}
	}
	return ret, nil
}

func Solve(filePath string, pe bool) error {
	if pe {
		printExample()
		return nil
	}

	g, _ := NewGraph(3, false, mtypes.GraphRecalculateSetting{}, mtypes.NTPInfo{}, mtypes.LoggerInfo{LogInternal: false})
	inputb, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	input := string(inputb)
	all_edge, _ := ParseDistanceMatrix(input)
	g.UpdateLatencyMulti(all_edge, false, false)
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
	all_vert := g.Vertices()
	for u := range all_vert {
		for v := range all_vert {
			if u != v {
				path, err := g.Path(u, v)
				pathstr := fmt.Sprint(path)
				if err != nil {
					pathstr = fmt.Sprint(path)
				}
				fmt.Printf("%d -> %d\t%3f\t%s\n", u, v, dist[u][v], pathstr)
			}
		}
	}
	return nil
}
