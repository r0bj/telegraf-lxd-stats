package main

import (
	"fmt"
	"net"
	"net/http"
	"io/ioutil"
	"encoding/json"
	"strings"
	"errors"
	"strconv"
	"reflect"
	"regexp"

	"github.com/shirou/gopsutil/mem"
)

type ContainersResponse struct {
	Metadata []string
}

type ContainerStateResponse struct {
	Metadata map[string]interface{}
}

type CgroupTask struct {
	lxcName string
	cgroupPath string
	cgroupItem string
}

type CgroupTaskResult struct {
	lxcName string
	cgroupItem string
	cgroupContent string
	err error
}

type HttpTaskResult struct {
	lxdName string
	stats map[string]uint64
	err error
}

func genLineProtMsg(m map[string]map[string]interface{}) string {
	output_list := make([]string, 0)
	for lxc_host, lxc_data := range m {
		lxc_data_array := make([]string, 0)

		for key, value := range lxc_data {
			if t := reflect.TypeOf(value); t.Kind() == reflect.Uint64 {
				lxc_data_array = append(lxc_data_array, fmt.Sprintf("%s=%d", key, value))
			} else if t := reflect.TypeOf(value); t.Kind() == reflect.Float64 {
				lxc_data_array = append(lxc_data_array, fmt.Sprintf("%s=%f", key, value))
			}
		}
		var line string
		if len(lxc_data_array) > 0 {
			line = "lxcstats,lxc_host=" + lxc_host + " " + strings.Join(lxc_data_array, ",")
			output_list = append(output_list, line)
		}
	}
	return strings.Join(output_list, "\n")
}

func strToUint64(s string) uint64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("Func strToUint64 fail for %s\n", s))
	}
	return uint64(i)
}

func fakeDial(proto, addr string) (conn net.Conn, err error) {
	return net.Dial("unix", "/var/lib/lxd/unix.socket")
}

func sendHttpReq(path string) []byte {
	tr := &http.Transport{
	    Dial: fakeDial,
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Get("http://fake" + path)
	if err != nil {
		panic("HTTP GET failed")
	}
	if resp.StatusCode != 200 {
		panic(fmt.Sprintf("HTTP response code: %s", resp.StatusCode))
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic("Reading HTTP body failed")
	}
	return body
}

func getLxdList() []string {
	body := sendHttpReq("/1.0/containers")

	var containerResponse ContainersResponse
	err := json.Unmarshal(body, &containerResponse)
	if err != nil {
		panic(fmt.Sprintf("Cannot unmarshal HTTP body: %s", err))
	}

	lxdList := make([]string, 0)
	for _, c := range containerResponse.Metadata {
		s := strings.Split(c, "/")
		lxdList = append(lxdList, s[len(s)-1])
	}
	return lxdList
}

func getLxdInterfaceCounters(lxd string, channel chan<- HttpTaskResult) {
	var httpTaskResult HttpTaskResult
	httpTaskResult.lxdName = lxd

	stats := make(map[string]map[string]uint64)
	body := sendHttpReq(fmt.Sprintf("/1.0/containers/%s/state", lxd))

	var containerStateResponse ContainerStateResponse
	err := json.Unmarshal(body, &containerStateResponse)
	if err != nil {
		panic(fmt.Sprintf("Cannot unmarshal HTTP body: %s", err))
	}

	if containerStateResponse.Metadata == nil {
		httpTaskResult.err = errors.New("Cannot get LXD metadata")
		channel <- httpTaskResult
		return
	}
	// LXD container stopped?
	if containerStateResponse.Metadata["network"] == nil {
		httpTaskResult.err = errors.New("Cannot get LXD network data")
		channel <- httpTaskResult
		return
	}
	for iface, value := range containerStateResponse.Metadata["network"].(map[string]interface{}) {
		if value.(map[string]interface{})["host_name"].(string) == "" {
			continue
		}
		if stats[iface] == nil {
			stats[iface] = make(map[string]uint64)
		}
		stats[iface]["rx"] = uint64(value.(map[string]interface{})["counters"].(map[string]interface{})["bytes_received"].(float64))
		stats[iface]["tx"] = uint64(value.(map[string]interface{})["counters"].(map[string]interface{})["bytes_sent"].(float64))
	}

	output := make(map[string]uint64)
	for _, value := range stats {
		output["tx"] += value["tx"]
		output["rx"] += value["rx"]
	}

	httpTaskResult.stats = output
	channel <- httpTaskResult
}

func readCgroupFile(task CgroupTask, channel chan<- CgroupTaskResult) {
	var t CgroupTaskResult
	t.lxcName = task.lxcName
	t.cgroupItem = task.cgroupItem
	content, err := ioutil.ReadFile(task.cgroupPath)
	if err == nil {
		t.cgroupContent = strings.TrimSpace(string(content))
	} else {
		t.err = errors.New(fmt.Sprintf("Cannot read %s", task.cgroupPath))
	}
	channel <- t
}

func genCgroupTaskList(lxdList []string) []CgroupTask {
	tasks := make([]CgroupTask, 0)
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "blkio.throttle.io_serviced"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/blkio/lxc/%s/blkio.throttle.io_serviced", lxd)
		tasks = append(tasks, t)
	}
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "blkio.throttle.io_service_bytes"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/blkio/lxc/%s/blkio.throttle.io_service_bytes", lxd)
		tasks = append(tasks, t)
	}
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "memory.usage_in_bytes"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/memory/lxc/%s/memory.usage_in_bytes", lxd)
		tasks = append(tasks, t)
	}
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "memory.limit_in_bytes"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/memory/lxc/%s/memory.limit_in_bytes", lxd)
		tasks = append(tasks, t)
	}
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "memory.memsw.usage_in_bytes"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/memory/lxc/%s/memory.memsw.usage_in_bytes", lxd)
		tasks = append(tasks, t)
	}
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "memory.memsw.limit_in_bytes"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/memory/lxc/%s/memory.memsw.limit_in_bytes", lxd)
		tasks = append(tasks, t)
	}
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "cpuacct.usage"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/cpu,cpuacct/lxc/%s/cpuacct.usage", lxd)
		tasks = append(tasks, t)
	}
	for _, lxd := range lxdList {
		var t CgroupTask
		t.cgroupItem = "cpuset.cpus"
		t.lxcName = lxd
		t.cgroupPath = fmt.Sprintf("/sys/fs/cgroup/cpuset/lxc/%s/cpuset.cpus", lxd)
		tasks = append(tasks, t)
	}
	return tasks
}

func getTotalMem() uint64 {
	virtual_mem, err := mem.VirtualMemory()
	if err != nil {
		panic("Cannot get total memory value")
	}
	return virtual_mem.Total
}

func blkioServiced(content []string) (map[string]uint64, error) {
	var read uint64 = 0
	var write uint64 = 0
	for _, v := range content {
		b := strings.Split(v, " ")
		if b[1] == "Read" {
			read += strToUint64(b[2])
		}
		if b[1] == "Write" {
			write += strToUint64(b[2])
		}
	}
	return map[string]uint64{"blkioServicedRead": read, "blkioServicedWrite": write}, nil
}

func blkioServiceBytes(content []string) (map[string]uint64, error) {
	var read uint64 = 0
	var write uint64 = 0
	for _, v := range content {
		b := strings.Split(v, " ")
		if b[1] == "Read" {
			read += strToUint64(b[2])
		}
		if b[1] == "Write" {
			write += strToUint64(b[2])
		}
	}
	return map[string]uint64{"blkioServiceReadBytes": read, "blkioServiceWriteBytes": write}, nil
}

func memUsage(content string) (uint64, error) {
	if content != "" {
		return strToUint64(content), nil
	}
	return 0, errors.New("memUsage for the container failed")
}

func memLimit(content string) (uint64, error) {
	total_memory := getTotalMem()
	if content != "" {
		value_uint64 := strToUint64(content)
		if value_uint64 > total_memory {
			return total_memory, nil
		} else {
			return value_uint64, nil
		}
	}
	return 0, errors.New("memLimit for the container failed")
}

func memUsagePerc(mem_usage, mem_limit float64) (float64, error) {
	if mem_limit > 0 {
		return mem_usage / mem_limit * 100, nil
	} else {
		return 0, errors.New("memUsagePerc for the container failed")
	}
}

func memswUsage(content string) (uint64, error) {
	if content != "" {
		return strToUint64(content), nil
	}
	return 0, errors.New("memswUsage for the container failed")
}

// TODO: add swap space to total_memory
func memswLimit(content string) (uint64, error) {
	total_memory := getTotalMem()
	if content != "" {
		value_uint64 := strToUint64(content)
		if value_uint64 > total_memory {
			return total_memory, nil
		} else {
			return value_uint64, nil
		}
	}
	return 0, errors.New("memswLimit for the container failed")
}

func cpuTime(content string) (uint64, error) {
	if content != "" {
		return strToUint64(content), nil
	}
	return 0, errors.New("cpuTime for the container failed")
}

func cpuTimePerCpu(content string, cpuTime float64) (float64, error) {
	if content != "" {
		if numCores := countCores(content); numCores > 0 {
			return cpuTime / float64(numCores), nil
		}
	}
	return 0, errors.New("cpuTimePerCpu for the container failed")
}

// takes string from cgroup cpuset.cpus and return number of cores, eg. takes "0-3,26" and return 5 
func countCores(cpus string) int {
	cpus_array := strings.Split(cpus, ",")
	r := regexp.MustCompile(`^(\d+)-(\d+)$`)
	var cntr int
	for _, entry := range cpus_array {
		if matches := r.FindStringSubmatch(entry); matches != nil {
			start_int := strToUint64(matches[1])
			stop_int := strToUint64(matches[2])
			for i := start_int; i <= stop_int; i++ {
				cntr++
			}
		} else if matches, _ := regexp.MatchString(`\d+`, entry); matches {
			cntr++
		}
	}
	return cntr
}

func findCgroupContent(cgroupItem, lxd string, list []CgroupTaskResult) (string, error) {
	for _, task := range list {
		if task.cgroupItem == cgroupItem && task.lxcName == lxd {
			return task.cgroupContent, nil
		}
	}
	return "", errors.New("Cannot find cgroup content")
}

func gatherCgroupData(lxdList []string, lxcData map[string]map[string]interface{}) []CgroupTaskResult {
	channel := make(chan CgroupTaskResult)
	cgroupTaskList := genCgroupTaskList(lxdList)
	for _, task := range cgroupTaskList {
		go readCgroupFile(task, channel)
	}

	CgroupTaskResults := make([]CgroupTaskResult, 0)
	for t := 0; t < len(cgroupTaskList); t++ {
		CgroupTaskResults = append(CgroupTaskResults, <- channel)
	}

	for _, lxd := range lxdList {
		lxcData[lxd] = make(map[string]interface{})
	}

	for _, t := range CgroupTaskResults {
		if t.err != nil {
			continue
		}
		if t.cgroupItem == "blkio.throttle.io_serviced" {
			data, err := blkioServiced(strings.Split(t.cgroupContent, "\n"))
			if err == nil {
				lxcData[t.lxcName]["blkio_reads"] = data["blkioServicedRead"]
				lxcData[t.lxcName]["blkio_writes"] = data["blkioServicedWrite"]
			}
		} else if t.cgroupItem == "blkio.throttle.io_service_bytes" {
			data, err := blkioServiceBytes(strings.Split(t.cgroupContent, "\n"))
			if err == nil {
				lxcData[t.lxcName]["blkio_read_bytes"] = data["blkioServiceReadBytes"]
				lxcData[t.lxcName]["blkio_write_bytes"] = data["blkioServiceWriteBytes"]
			}
		} else if t.cgroupItem == "memory.usage_in_bytes" {
			data, err := memUsage(t.cgroupContent)
			if err == nil {
				lxcData[t.lxcName]["mem_usage"] = data
			}
		} else if t.cgroupItem == "memory.limit_in_bytes" {
			data, err := memLimit(t.cgroupContent)
			if err == nil {
				lxcData[t.lxcName]["mem_limit"] = data
			}
		} else if t.cgroupItem == "memory.memsw.usage_in_bytes" {
			data, err := memswUsage(t.cgroupContent)
			if err == nil {
				lxcData[t.lxcName]["memsw_usage"] = data
			}
		} else if t.cgroupItem == "memory.memsw.limit_in_bytes" {
			data, err := memswLimit(t.cgroupContent)
			if err == nil {
				lxcData[t.lxcName]["memsw_limit"] = data
			}
		} else if t.cgroupItem == "cpuacct.usage" {
			data, err := cpuTime(t.cgroupContent)
			if err == nil {
				lxcData[t.lxcName]["cpu_time"] = data
			}
		}
	}
	return CgroupTaskResults
}

func gatherComplexData(lxdList []string, lxcData map[string]map[string]interface{}, cgroupTaskResults []CgroupTaskResult) {
	for _, lxd := range lxdList {
		_, okUsage := lxcData[lxd]["mem_usage"]
		_, okLimit := lxcData[lxd]["mem_limit"]

		if okUsage && okLimit {
			memUsagePerc, err := memUsagePerc(float64(lxcData[lxd]["mem_usage"].(uint64)), float64(lxcData[lxd]["mem_limit"].(uint64)))
			if err == nil {
				lxcData[lxd]["mem_usage_perc"] = memUsagePerc
			}
		}

		_, okUsageSw := lxcData[lxd]["memsw_usage"]
		_, okLimitSw := lxcData[lxd]["memsw_limit"]

		if okUsageSw && okLimitSw {
			memUsagePerc, err := memUsagePerc(float64(lxcData[lxd]["memsw_usage"].(uint64)), float64(lxcData[lxd]["memsw_limit"].(uint64)))
			if err == nil {
				lxcData[lxd]["memsw_usage_perc"] = memUsagePerc
			}
		}

		_, okTime := lxcData[lxd]["cpu_time"]
		if okTime {
			cgroupContent, err := findCgroupContent("cpuset.cpus", lxd, cgroupTaskResults)
			if err == nil {
				data, err := cpuTimePerCpu(cgroupContent, float64(lxcData[lxd]["cpu_time"].(uint64)))
				if err == nil {
					lxcData[lxd]["cpu_time_percpu"] = data
				}
			}		
		}
	}
}

func gatherApiData(lxdList []string, lxcData map[string]map[string]interface{}) {
	channel := make(chan HttpTaskResult)
	for _, lxd := range lxdList {
		go getLxdInterfaceCounters(lxd, channel)
	}

	httpTaskResults := make([]HttpTaskResult, 0)
	for l := 0; l < len(lxdList); l++ {
		httpTaskResults = append(httpTaskResults, <- channel)
	}

	for _, result := range httpTaskResults {
		if result.err == nil {
			/* tx and rx are reversed from the host vs container */
			lxcData[result.lxdName]["bytes_sent"] = result.stats["rx"]
			lxcData[result.lxdName]["bytes_recv"] = result.stats["tx"]
		}
	}	
}

func main() {
	lxdList := getLxdList()
	lxcData := make(map[string]map[string]interface{})

	cgroupTaskResults := gatherCgroupData(lxdList, lxcData)
	gatherComplexData(lxdList, lxcData, cgroupTaskResults)
	gatherApiData(lxdList, lxcData)

	fmt.Printf("%s\n", genLineProtMsg(lxcData))
}
