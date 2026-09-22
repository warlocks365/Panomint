package main

// 断连即时提示 v2（Job000072）：挂载存活探针。
//
// v1 语义：rclone 进程死亡=FUSE 挂载自动消失（/proc/mounts 看得到），但**源端断连**
// 挂载仍 online，要等 5min 同步周期报错才感知。v2 给每个在线挂载加主动探针：
//
//   - webdav/smb（rclone FUSE）：挂载时带 --rc 起 HTTP 控制口（仅 127.0.0.1、免认证、
//     每挂载确定性端口），探针走 rc operations/list 直打后端（不经 FUSE 读路径——
//     FUSE 在后端不可达时会挂起且无 ctx 可救，rc 则受 --contimeout/--timeout 约束快速失败）。
//   - nfs（内核挂载）：无 rc 可言，探针=stat 挂载根。必须 soft 挂载
//     （ro,soft,timeo=50,retrans=2 → 最坏 ~15s 返回错误而非无限重试），否则
//     服务端断连后 stat 会把对账循环永久卡死在内核态。
//
// 探针失败 → status=offline + last_error；恢复 → 回 online。状态翻转才写库
//（probeBad 状态机），避免探针成功把同步刚写的 error 状态擦掉。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"os"
	"time"
)

// rcBasePort/rccPortRange：rc 监听端口段（127.0.0.1 only）。每挂载确定性分配，
// 便于排查（docker exec 进容器 curl 对应端口即可手动复核）。
const (
	rcBasePort  = 5572
	rcPortRange = 400
	rcTimeout   = 5 * time.Second
)

// rcPort 由挂载键确定性推导 rc 端口（同键恒同口；冲突时后者 rclone 绑定失败会
// 在挂载阶段显式报错，不会静默串台）。
func rcPort(key string) int {
	return rcBasePort + int(crc32.ChecksumIEEE([]byte(key))%rcPortRange)
}

// httpClient 全包共用（短超时，防任何网络悬停拖死对账循环）。
var httpClient = &http.Client{Timeout: rcTimeout}

// rcProbe 经 rclone rc 直打后端列根目录：证明「进程活着 + 后端可达」。
// 不经 FUSE 读路径（后端不可达时 FUSE 请求挂起无 ctx 可救——v1 实测）。
func rcProbe(port int) error {
	body, _ := json.Marshal(map[string]string{"fs": "dst:", "remote": ""})
	url := fmt.Sprintf("http://127.0.0.1:%d/operations/list", port)
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("rc 不可达: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rc HTTP %d: %s", resp.StatusCode, truncate(out.Error, 200))
	}
	if out.Error != "" {
		return fmt.Errorf("后端不可达: %s", truncate(out.Error, 200))
	}
	return nil
}

// statProbe NFS 探针：stat 挂载根。依赖 soft 挂载选项保证有界返回。
func statProbe(mp string) error {
	_, err := os.Stat(mp)
	if err != nil {
		return fmt.Errorf("stat 挂载点: %w", err)
	}
	return nil
}

// probeMount 按类型分发探针。
func probeMount(typ, key, mp string) error {
	if typ == "nfs" {
		return statProbe(mp)
	}
	return rcProbe(rcPort(key))
}
