package collector

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Identity struct {
	Name         string `json:"name,omitempty"`
	Hostname     string `json:"hostname"`
	MachineID    string `json:"machine_id,omitempty"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Kernel       string `json:"kernel,omitempty"`
	AgentVersion string `json:"agent_version"`
}

type Report struct {
	CollectedAt string                 `json:"collected_at"`
	Identity    Identity               `json:"identity"`
	System      map[string]interface{} `json:"system"`
	Services    map[string]interface{} `json:"services"`
}

func CollectIdentity(name, version string) Identity {
	hostname, _ := os.Hostname()
	return Identity{
		Name:         firstNonEmpty(name, hostname),
		Hostname:     hostname,
		MachineID:    readFirst("/etc/machine-id", "/var/lib/dbus/machine-id"),
		OS:           osReleaseName(),
		Arch:         runtime.GOARCH,
		Kernel:       strings.TrimSpace(runText(2*time.Second, "uname", "-r")),
		AgentVersion: version,
	}
}

func CollectReport(name, version string) Report {
	identity := CollectIdentity(name, version)
	return Report{
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		Identity:    identity,
		System: map[string]interface{}{
			"uptime_seconds": uptimeSeconds(),
			"load":           loadAvg(),
			"memory":         memoryInfo(),
			"disk":           diskInfo("/"),
		},
		Services: map[string]interface{}{
			"systemd": systemdServices(),
			"docker":  dockerContainers(),
			"ports":   listeningPorts(),
			"signals": serviceSignals(),
		},
	}
}

func systemdServices() []map[string]string {
	out := runText(5*time.Second, "systemctl", "--type=service", "--state=running", "--no-legend", "--no-pager")
	rows := []map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		rows = append(rows, map[string]string{
			"unit":        fields[0],
			"load":        fieldAt(fields, 1),
			"active":      fieldAt(fields, 2),
			"state":       fieldAt(fields, 3),
			"description": strings.Join(fields[4:], " "),
		})
		if len(rows) >= 80 {
			break
		}
	}
	return rows
}

func dockerContainers() []map[string]string {
	out := runText(5*time.Second, "docker", "ps", "--format", "{{.ID}}\t{{.Image}}\t{{.Names}}\t{{.Status}}\t{{.Ports}}")
	rows := []map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}
		rows = append(rows, map[string]string{
			"id":     parts[0],
			"image":  fieldAt(parts, 1),
			"name":   fieldAt(parts, 2),
			"status": fieldAt(parts, 3),
			"ports":  fieldAt(parts, 4),
		})
		if len(rows) >= 80 {
			break
		}
	}
	return rows
}

func listeningPorts() []map[string]string {
	out := runText(5*time.Second, "ss", "-H", "-tulpn")
	rows := []map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		process := ""
		if len(fields) >= 7 {
			process = strings.Join(fields[6:], " ")
		}
		rows = append(rows, map[string]string{
			"protocol": fields[0],
			"state":    fieldAt(fields, 1),
			"local":    fieldAt(fields, 4),
			"peer":     fieldAt(fields, 5),
			"process":  process,
			"raw":      line,
		})
		if len(rows) >= 120 {
			break
		}
	}
	return rows
}

func serviceSignals() map[string]string {
	signals := map[string]string{}
	if out := strings.TrimSpace(runText(3*time.Second, "caddy", "version")); out != "" {
		signals["caddy"] = out
	}
	if out := strings.TrimSpace(runText(3*time.Second, "nginx", "-v")); out != "" {
		signals["nginx"] = out
	}
	return signals
}

func memoryInfo() map[string]uint64 {
	result := map[string]uint64{}
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return result
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(strings.TrimSuffix(scanner.Text(), ":"))
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			result[key] = value * 1024
		}
	}
	return result
}

func diskInfo(path string) map[string]uint64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return map[string]uint64{}
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	return map[string]uint64{"total": total, "free": free, "used": total - free}
}

func uptimeSeconds() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	first := strings.Fields(string(data))
	if len(first) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(first[0], 64)
	return uint64(value)
}

func loadAvg() []float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return []float64{}
	}
	fields := strings.Fields(string(data))
	values := []float64{}
	for i := 0; i < len(fields) && i < 3; i++ {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err == nil {
			values = append(values, value)
		}
	}
	return values
}

func osReleaseName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

func readFirst(paths ...string) string {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func runText(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return string(out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fieldAt(values []string, idx int) string {
	if idx < 0 || idx >= len(values) {
		return ""
	}
	return values[idx]
}
