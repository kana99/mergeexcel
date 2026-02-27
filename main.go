package main

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kataras/iris/v12"
)

type Device struct {
	IP          string    `json:"ip"`
	Name        string    `json:"name"`
	Online      bool      `json:"online"`
	LastChecked time.Time `json:"lastChecked"`
}

type Monitor struct {
	mu      sync.RWMutex
	devices map[string]*Device
}

func NewMonitor() *Monitor {
	devices := make(map[string]*Device, 254)
	for i := 1; i <= 254; i++ {
		ip := fmt.Sprintf("10.5.2.%d", i)
		devices[ip] = &Device{IP: ip, Name: ip}
	}
	return &Monitor{devices: devices}
}

func (m *Monitor) List() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]Device, 0, len(m.devices))
	for _, d := range m.devices {
		list = append(list, *d)
	}

	sort.Slice(list, func(i, j int) bool {
		return compareIPv4(list[i].IP, list[j].IP)
	})
	return list
}

func compareIPv4(a, b string) bool {
	aIP := net.ParseIP(a).To4()
	bIP := net.ParseIP(b).To4()
	for i := 0; i < 4; i++ {
		if aIP[i] == bIP[i] {
			continue
		}
		return aIP[i] < bIP[i]
	}
	return false
}

func (m *Monitor) SetName(ip, name string) bool {
	if net.ParseIP(ip) == nil || name == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[ip]
	if !ok {
		return false
	}
	d.Name = name
	return true
}

func (m *Monitor) ScanAll() {
	ips := make([]string, 0, 254)
	for i := 1; i <= 254; i++ {
		ips = append(ips, fmt.Sprintf("10.5.2.%d", i))
	}

	const workers = 32
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				online := pingHost(ip)
				m.mu.Lock()
				if d, ok := m.devices[ip]; ok {
					d.Online = online
					d.LastChecked = time.Now()
				}
				m.mu.Unlock()
			}
		}()
	}

	for _, ip := range ips {
		jobs <- ip
	}
	close(jobs)
	wg.Wait()
}

func pingHost(ip string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("ping", "-n", "1", "-w", "1000", ip)
	case "darwin":
		cmd = exec.Command("ping", "-c", "1", "-W", "1000", ip)
	default:
		cmd = exec.Command("ping", "-c", "1", "-W", "1", ip)
	}
	return cmd.Run() == nil
}

func main() {
	app := iris.New()
	monitor := NewMonitor()

	go func() {
		monitor.ScanAll()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			monitor.ScanAll()
		}
	}()

	app.Get("/", func(ctx iris.Context) { ctx.HTML(pageHTML) })
	app.Get("/api/devices", func(ctx iris.Context) { ctx.JSON(monitor.List()) })
	app.Post("/api/devices/{ip:string}/name", func(ctx iris.Context) {
		ip := ctx.Params().Get("ip")
		name := strings.TrimSpace(ctx.FormValue("name"))
		if name == "" {
			var payload struct{ Name string `json:"name"` }
			if err := ctx.ReadJSON(&payload); err == nil {
				name = strings.TrimSpace(payload.Name)
			}
		}
		if !monitor.SetName(ip, name) {
			ctx.StopWithStatus(iris.StatusBadRequest)
			return
		}
		ctx.StatusCode(iris.StatusNoContent)
	})

	_ = app.Listen(":8080")
}

const pageHTML = `<!doctype html>
<html lang="zh-CN"><head>
<meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" />
<title>10.5.2.x 在线监控</title>
<style>
body { font-family: Arial, sans-serif; margin: 24px; background: #f7f9fc; color: #1f2937; }
h1 { margin-bottom: 8px; }.hint { color: #6b7280; margin-bottom: 16px; }
table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; }
th, td { padding: 10px 12px; border-bottom: 1px solid #e5e7eb; text-align: left; }
th { background: #f3f4f6; }.status-dot { width: 14px; height: 14px; border-radius: 50%; display: inline-block; }
.online { background: #16a34a; }.offline { background: #dc2626; }
.name-edit { width: 100%; max-width: 260px; padding: 6px; }
button { padding: 6px 10px; border: 1px solid #d1d5db; background: #fff; border-radius: 6px; cursor: pointer; }
</style></head>
<body>
<h1>10.5.2.x 网段设备监控</h1>
<div class="hint">每隔1小时自动检测一次在线状态（启动后会先立即检测一次）。</div>
<table><thead><tr><th>状态</th><th>IP</th><th>设备名称</th><th>最近检测时间</th><th>操作</th></tr></thead><tbody id="tbody"></tbody></table>
<script>
async function loadDevices() {
  const res = await fetch('/api/devices');
  const devices = await res.json();
  const tbody = document.getElementById('tbody');
  tbody.innerHTML = '';
  for (const d of devices) {
    const tr = document.createElement('tr');
    const checked = d.lastChecked ? new Date(d.lastChecked).toLocaleString() : '-';
    const statusClass = d.online ? 'online' : 'offline';
    tr.innerHTML = '<td><span class="status-dot ' + statusClass + '"></span></td>' +
      '<td>' + d.ip + '</td>' +
      '<td><input class="name-edit" id="name-' + d.ip + '" value="' + d.name + '" /></td>' +
      '<td>' + checked + '</td>' +
      '<td><button onclick="saveName(\'' + d.ip + '\')">保存</button></td>';
    tbody.appendChild(tr);
  }
}

async function saveName(ip) {
  const input = document.getElementById('name-' + ip);
  const name = input.value.trim();
  if (!name) { alert('名称不能为空'); return; }
  const res = await fetch('/api/devices/' + ip + '/name', {
    method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({name})
  });
  if (!res.ok) { alert('保存失败'); return; }
  await loadDevices();
}
loadDevices();
setInterval(loadDevices, 30000);
</script>
</body></html>`
