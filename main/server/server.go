package main

import (
	"flag"
	"github.com/acharapko/pbench"
	"github.com/acharapko/pbench/cfg"
	"github.com/acharapko/pbench/idservice"
	"github.com/acharapko/pbench/log"
	"github.com/acharapko/pbench/protocols/batchedpaxos"
	"github.com/acharapko/pbench/protocols/epaxos"
	"github.com/acharapko/pbench/protocols/paxos"
	"io/ioutil"
	//"runtime/debug"
	"sync"
	"strconv"
	"strings"
	"time"
	"github.com/acharapko/pbench/protocols/pigpaxos"
	"net/http"
	l "log"
	_ "net/http/pprof"
)

var algorithm = flag.String("algorithm", "paxos", "Distributed algorithm")
var id = flag.String("id", "", "NodeId in format of Zone.Node.")
var simulation = flag.Bool("sim", false, "simulation mode")
var profile = flag.Bool("p", false, "use pprof")

func replica(id idservice.ID) {

	log.Infof("node %v starting with algorithm %s", id, *algorithm)
	if *profile {
		go func() {
			l.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}


	switch *algorithm {

	case "paxos":
		paxos.NewReplica(id).Run()
	case "batchedpaxos":
		batchedpaxos.NewReplica(id).Run()
	case "pigpaxos":
		pigpaxos.NewReplica(id).Run()
	case "epaxos":
		epaxos.NewReplica(id).Run()
	default:
		panic("Unknown algorithm")
	}
}

func getCpuSample() (idle uint64, total uint64) {
	contents, err := ioutil.ReadFile("/proc/stat")
	if err != nil {
		log.Errorf("node %v proc stat read %v ", id, err)
		return
	}
	lines := strings.Split(string(contents), "\n")
	//cpu user nice system idle io irq softirq
	for _, line := range(lines) {
		fields := strings.Fields(line)
		if fields[0] == "cpu" {
			for i := 1; i < len(fields); i++ {
				time, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					log.Errorf("node %v Atoi error %s ", id, line)
				}
				total += time
				if i == 4 {
					idle = time
				}
			}
			return
		}
	}
	return
}

func cpuUtilization(id idservice.ID) {
	idlePrev, totalPrev := getCpuSample()
	for{
		time.Sleep(1 * time.Second)
		idleCurr, totalCurr := getCpuSample()
		idleTime  := float64(idleCurr - idlePrev)
		totalTime := float64(totalCurr - totalPrev)
		cpuUtilization := 100 * (totalTime - idleTime)/totalTime
		idlePrev, totalPrev = idleCurr, totalCurr
		log.Infof("node %v CPU utilization: %f ", id, cpuUtilization)
	}
}


func main() {
	pbench.Init()
	//debug.SetGCPercent(-1)
	if *simulation {
		var wg sync.WaitGroup
		wg.Add(1)
		//Simulation()
		for id := range cfg.GetConfig().Addrs {
			n := id
			go replica(n)
		}
		wg.Wait()
	} else {
		go cpuUtilization(idservice.NewIDFromString(*id))
		replica(idservice.NewIDFromString(*id))
		log.Debugf("Server done")
	}
}
