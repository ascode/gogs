// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package admin

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/Unknwon/com"
	"gopkg.in/macaron.v1"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/pkg/context"
	"github.com/gogits/gogs/pkg/cron"
	"github.com/gogits/gogs/pkg/mailer"
	"github.com/gogits/gogs/pkg/process"
	"github.com/gogits/gogs/pkg/setting"
	"github.com/gogits/gogs/pkg/tool"
)

const (
	DASHBOARD = "admin/dashboard"
	CONFIG    = "admin/config"
	MONITOR   = "admin/monitor"
)

var (
	startTime = time.Now()
)

var sysStatus struct {
	Uptime       string
	NumGoroutine int

	// General statistics.
	MemAllocated string // bytes allocated and still in use
	MemTotal     string // bytes allocated (even if freed)
	MemSys       string // bytes obtained from system (sum of XxxSys below)
	Lookups      uint64 // number of pointer lookups
	MemMallocs   uint64 // number of mallocs
	MemFrees     uint64 // number of frees

	// Main allocation heap statistics.
	HeapAlloc    string // bytes allocated and still in use
	HeapSys      string // bytes obtained from system
	HeapIdle     string // bytes in idle spans
	HeapInuse    string // bytes in non-idle span
	HeapReleased string // bytes released to the OS
	HeapObjects  uint64 // total number of allocated objects

	// Low-level fixed-size structure allocator statistics.
	//	Inuse is bytes used now.
	//	Sys is bytes obtained from system.
	StackInuse  string // bootstrap stacks
	StackSys    string
	MSpanInuse  string // mspan structures
	MSpanSys    string
	MCacheInuse string // mcache structures
	MCacheSys   string
	BuckHashSys string // profiling bucket hash table
	GCSys       string // GC metadata
	OtherSys    string // other system allocations

	// Garbage collector statistics.
	NextGC       string // next run in HeapAlloc time (bytes)
	LastGC       string // last run in absolute time (ns)
	PauseTotalNs string
	PauseNs      string // circular buffer of recent GC pause times, most recent at [(NumGC+255)%256]
	NumGC        uint32
}

func updateSystemStatus() {
	sysStatus.Uptime = tool.TimeSincePro(startTime)

	m := new(runtime.MemStats)
	runtime.ReadMemStats(m)
	sysStatus.NumGoroutine = runtime.NumGoroutine()

	sysStatus.MemAllocated = tool.FileSize(int64(m.Alloc))
	sysStatus.MemTotal = tool.FileSize(int64(m.TotalAlloc))
	sysStatus.MemSys = tool.FileSize(int64(m.Sys))
	sysStatus.Lookups = m.Lookups
	sysStatus.MemMallocs = m.Mallocs
	sysStatus.MemFrees = m.Frees

	sysStatus.HeapAlloc = tool.FileSize(int64(m.HeapAlloc))
	sysStatus.HeapSys = tool.FileSize(int64(m.HeapSys))
	sysStatus.HeapIdle = tool.FileSize(int64(m.HeapIdle))
	sysStatus.HeapInuse = tool.FileSize(int64(m.HeapInuse))
	sysStatus.HeapReleased = tool.FileSize(int64(m.HeapReleased))
	sysStatus.HeapObjects = m.HeapObjects

	sysStatus.StackInuse = tool.FileSize(int64(m.StackInuse))
	sysStatus.StackSys = tool.FileSize(int64(m.StackSys))
	sysStatus.MSpanInuse = tool.FileSize(int64(m.MSpanInuse))
	sysStatus.MSpanSys = tool.FileSize(int64(m.MSpanSys))
	sysStatus.MCacheInuse = tool.FileSize(int64(m.MCacheInuse))
	sysStatus.MCacheSys = tool.FileSize(int64(m.MCacheSys))
	sysStatus.BuckHashSys = tool.FileSize(int64(m.BuckHashSys))
	sysStatus.GCSys = tool.FileSize(int64(m.GCSys))
	sysStatus.OtherSys = tool.FileSize(int64(m.OtherSys))

	sysStatus.NextGC = tool.FileSize(int64(m.NextGC))
	sysStatus.LastGC = fmt.Sprintf("%.1fs", float64(time.Now().UnixNano()-int64(m.LastGC))/1000/1000/1000)
	sysStatus.PauseTotalNs = fmt.Sprintf("%.1fs", float64(m.PauseTotalNs)/1000/1000/1000)
	sysStatus.PauseNs = fmt.Sprintf("%.3fs", float64(m.PauseNs[(m.NumGC+255)%256])/1000/1000/1000)
	sysStatus.NumGC = m.NumGC
}

// Operation types.
type AdminOperation int

const (
	CLEAN_INACTIVATE_USER AdminOperation = iota + 1
	CLEAN_REPO_ARCHIVES
	CLEAN_MISSING_REPOS
	GIT_GC_REPOS
	SYNC_SSH_AUTHORIZED_KEY
	SYNC_REPOSITORY_HOOKS
	REINIT_MISSING_REPOSITORY
)

func Dashboard(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.dashboard")
	ctx.Data["PageIsAdmin"] = true
	ctx.Data["PageIsAdminDashboard"] = true

	// Run operation.
	op, _ := com.StrTo(ctx.Query("op")).Int()
	if op > 0 {
		var err error
		var success string

		switch AdminOperation(op) {
		case CLEAN_INACTIVATE_USER:
			success = ctx.Tr("admin.dashboard.delete_inactivate_accounts_success")
			err = models.DeleteInactivateUsers()
		case CLEAN_REPO_ARCHIVES:
			success = ctx.Tr("admin.dashboard.delete_repo_archives_success")
			err = models.DeleteRepositoryArchives()
		case CLEAN_MISSING_REPOS:
			success = ctx.Tr("admin.dashboard.delete_missing_repos_success")
			err = models.DeleteMissingRepositories()
		case GIT_GC_REPOS:
			success = ctx.Tr("admin.dashboard.git_gc_repos_success")
			err = models.GitGcRepos()
		case SYNC_SSH_AUTHORIZED_KEY:
			success = ctx.Tr("admin.dashboard.resync_all_sshkeys_success")
			err = models.RewriteAllPublicKeys()
		case SYNC_REPOSITORY_HOOKS:
			success = ctx.Tr("admin.dashboard.resync_all_hooks_success")
			err = models.SyncRepositoryHooks()
		case REINIT_MISSING_REPOSITORY:
			success = ctx.Tr("admin.dashboard.reinit_missing_repos_success")
			err = models.ReinitMissingRepositories()
		}

		if err != nil {
			ctx.Flash.Error(err.Error())
		} else {
			ctx.Flash.Success(success)
		}
		ctx.Redirect(setting.AppSubURL + "/admin")
		return
	}

	ctx.Data["Stats"] = models.GetStatistic()
	// FIXME: update periodically
	updateSystemStatus()
	ctx.Data["SysStatus"] = sysStatus
	ctx.HTML(200, DASHBOARD)
}

func SendTestMail(ctx *context.Context) {
	email := ctx.Query("email")
	// Send a test email to the user's email address and redirect back to Config
	if err := mailer.SendTestMail(email); err != nil {
		ctx.Flash.Error(ctx.Tr("admin.config.test_mail_failed", email, err))
	} else {
		ctx.Flash.Info(ctx.Tr("admin.config.test_mail_sent", email))
	}

	ctx.Redirect(setting.AppSubURL + "/admin/config")
}

func Config(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.config")
	ctx.Data["PageIsAdmin"] = true
	ctx.Data["PageIsAdminConfig"] = true

	ctx.Data["AppURL"] = setting.AppURL
	ctx.Data["Domain"] = setting.Domain
	ctx.Data["OfflineMode"] = setting.OfflineMode
	ctx.Data["DisableRouterLog"] = setting.DisableRouterLog
	ctx.Data["RunUser"] = setting.RunUser
	ctx.Data["RunMode"] = strings.Title(macaron.Env)
	ctx.Data["StaticRootPath"] = setting.StaticRootPath
	ctx.Data["LogRootPath"] = setting.LogRootPath
	ctx.Data["ReverseProxyAuthUser"] = setting.ReverseProxyAuthUser

	ctx.Data["SSH"] = setting.SSH

	ctx.Data["RepoRootPath"] = setting.RepoRootPath
	ctx.Data["ScriptType"] = setting.ScriptType
	ctx.Data["Repository"] = setting.Repository

	ctx.Data["Service"] = setting.Service
	ctx.Data["DbCfg"] = models.DbCfg
	ctx.Data["Webhook"] = setting.Webhook

	ctx.Data["MailerEnabled"] = false
	if setting.MailService != nil {
		ctx.Data["MailerEnabled"] = true
		ctx.Data["Mailer"] = setting.MailService
	}

	ctx.Data["CacheAdapter"] = setting.CacheAdapter
	ctx.Data["CacheInterval"] = setting.CacheInterval
	ctx.Data["CacheConn"] = setting.CacheConn

	ctx.Data["SessionConfig"] = setting.SessionConfig

	ctx.Data["DisableGravatar"] = setting.DisableGravatar
	ctx.Data["EnableFederatedAvatar"] = setting.EnableFederatedAvatar

	ctx.Data["GitVersion"] = setting.Git.Version
	ctx.Data["Git"] = setting.Git

	type logger struct {
		Mode, Config string
	}
	loggers := make([]*logger, len(setting.LogModes))
	for i := range setting.LogModes {
		loggers[i] = &logger{
			Mode: strings.Title(setting.LogModes[i]),
		}

		result, _ := json.MarshalIndent(setting.LogConfigs[i], "", "  ")
		loggers[i].Config = string(result)
	}
	ctx.Data["Loggers"] = loggers

	ctx.HTML(200, CONFIG)
}

func Monitor(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.monitor")
	ctx.Data["PageIsAdmin"] = true
	ctx.Data["PageIsAdminMonitor"] = true
	ctx.Data["Processes"] = process.Processes
	ctx.Data["Entries"] = cron.ListTasks()
	ctx.HTML(200, MONITOR)
}
