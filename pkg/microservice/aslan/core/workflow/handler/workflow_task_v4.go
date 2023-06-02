/*
Copyright 2022 The KodeRover Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/allegro/bigcache"
	"github.com/docker/distribution/uuid"
	"github.com/gin-gonic/gin"
	"github.com/koderover/zadig/pkg/microservice/aslan/core/code/service"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	commonmodels "github.com/koderover/zadig/pkg/microservice/aslan/core/common/repository/models"
	"github.com/koderover/zadig/pkg/microservice/aslan/core/workflow/service/workflow"
	"github.com/koderover/zadig/pkg/setting"
	internalhandler "github.com/koderover/zadig/pkg/shared/handler"
	e "github.com/koderover/zadig/pkg/tool/errors"
	"github.com/koderover/zadig/pkg/tool/log"
)

type listWorkflowTaskV4Query struct {
	PageSize     int64  `json:"page_size"    form:"page_size,default=20"`
	PageNum      int64  `json:"page_num"     form:"page_num,default=1"`
	WorkflowName string `json:"workflow_name" form:"workflow_name"`
}

type listWorkflowTaskV4Resp struct {
	WorkflowList []*commonmodels.WorkflowTask `json:"workflow_list"`
	Total        int64                        `json:"total"`
}

type ApproveRequest struct {
	StageName    string `json:"stage_name"`
	WorkflowName string `json:"workflow_name"`
	TaskID       int64  `json:"task_id"`
	Approve      bool   `json:"approve"`
	Comment      string `json:"comment"`
}

func CreateWorkflowTaskV4(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	args := new(commonmodels.WorkflowV4)
	data := getBody(c)
	if err := json.Unmarshal([]byte(data), args); err != nil {
		log.Errorf("CreateWorkflowTaskv4 json.Unmarshal err : %s", err)
		ctx.Err = e.ErrInvalidParam.AddDesc(err.Error())
		return
	}

	internalhandler.InsertOperationLog(c, ctx.UserName, args.Project, "新建", "自定义工作流任务", args.Name, data, ctx.Logger)
	ctx.Resp, ctx.Err = workflow.CreateWorkflowTaskV4(&workflow.CreateWorkflowTaskV4Args{
		Name:   ctx.UserName,
		UserID: ctx.UserID,
	}, args, ctx.Logger)
}

func CreateWorkflowTaskV4ByBuildInTrigger(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	args := new(commonmodels.WorkflowV4)
	if err := c.ShouldBindJSON(&args); err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc(err.Error())
		return
	}
	triggerName := c.Query("triggerName")
	if triggerName == "" {
		triggerName = setting.DefaultTaskRevoker
	}
	internalhandler.InsertOperationLog(c, ctx.UserName, args.Project, "新建", "自定义工作流任务", args.Name, getBody(c), ctx.Logger)
	ctx.Resp, ctx.Err = workflow.CreateWorkflowTaskV4ByBuildInTrigger(triggerName, args, ctx.Logger)
}

func ListWorkflowTaskV4(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()
	args := &listWorkflowTaskV4Query{}
	if err := c.ShouldBindQuery(args); err != nil {
		ctx.Err = err
		return
	}

	taskList, total, err := workflow.ListWorkflowTaskV4(args.WorkflowName, args.PageNum, args.PageSize, ctx.Logger)
	resp := listWorkflowTaskV4Resp{
		WorkflowList: taskList,
		Total:        total,
	}
	ctx.Resp = resp
	ctx.Err = err
}

func GetWorkflowTaskV4(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	taskID, err := strconv.ParseInt(c.Param("taskID"), 10, 64)
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid task id")
		return
	}
	ctx.Resp, ctx.Err = workflow.GetWorkflowTaskV4(c.Param("workflowName"), taskID, ctx.Logger)
}

func CancelWorkflowTaskV4(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	taskID, err := strconv.ParseInt(c.Param("taskID"), 10, 64)
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid task id")
		return
	}

	username := ctx.UserName
	if c.Query("username") != "" {
		username = c.Query("username")
	}
	internalhandler.InsertOperationLog(c, username, c.Query("projectName"), "取消", "自定义工作流任务", c.Param("workflowName"), "", ctx.Logger)
	ctx.Err = workflow.CancelWorkflowTaskV4(username, c.Param("workflowName"), taskID, ctx.Logger)
}

func CloneWorkflowTaskV4(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	taskID, err := strconv.ParseInt(c.Param("taskID"), 10, 64)
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid task id")
		return
	}
	internalhandler.InsertOperationLog(c, ctx.UserName, c.Query("projectName"), "克隆", "自定义工作流任务", c.Param("workflowName"), "", ctx.Logger)
	ctx.Resp, ctx.Err = workflow.CloneWorkflowTaskV4(c.Param("workflowName"), taskID, ctx.Logger)
}

func SetWorkflowTaskV4Breakpoint(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	taskID, err := strconv.ParseInt(c.Param("taskID"), 10, 64)
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid task id")
		return
	}
	var set bool
	switch c.Query("operation") {
	case "set", "unset":
		set = c.Query("operation") == "set"
	default:
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid operation")
		return
	}
	switch c.Param("position") {
	case "before", "after":
	default:
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid position")
		return
	}
	ctx.Err = workflow.SetWorkflowTaskV4Breakpoint(c.Param("workflowName"), c.Param("jobName"), taskID, set, c.Param("position"), ctx.Logger)
}

func EnableDebugWorkflowTaskV4(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	taskID, err := strconv.ParseInt(c.Param("taskID"), 10, 64)
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid task id")
		return
	}
	ctx.Err = workflow.EnableDebugWorkflowTaskV4(c.Param("workflowName"), taskID, ctx.Logger)
}

func StopDebugWorkflowTaskJobV4(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	taskID, err := strconv.ParseInt(c.Param("taskID"), 10, 64)
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid task id")
		return
	}
	ctx.Err = workflow.StopDebugWorkflowTaskJobV4(c.Param("workflowName"), c.Param("jobName"), taskID, c.Param("position"), ctx.Logger)
}

func ApproveStage(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()
	args := &ApproveRequest{}

	data, err := c.GetRawData()
	if err != nil {
		log.Errorf("ApproveStage c.GetRawData() err : %s", err)
	}
	if err = json.Unmarshal(data, args); err != nil {
		log.Errorf("ApproveStage json.Unmarshal err : %s", err)
	}

	c.Request.Body = io.NopCloser(bytes.NewBuffer(data))

	if err := c.ShouldBindJSON(&args); err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc(err.Error())
		return
	}

	ctx.Err = workflow.ApproveStage(args.WorkflowName, args.StageName, ctx.UserName, ctx.UserID, args.Comment, args.TaskID, args.Approve, ctx.Logger)
}

func GetWorkflowV4ArtifactFileContent(c *gin.Context) {
	ctx := internalhandler.NewContext(c)
	defer func() { internalhandler.JSONResponse(c, ctx) }()

	taskID, err := strconv.ParseInt(c.Param("taskId"), 10, 64)
	if err != nil {
		ctx.Err = e.ErrInvalidParam.AddDesc("invalid task id")
		return
	}

	resp, err := workflow.GetWorkflowV4ArtifactFileContent(c.Param("workflowName"), c.Param("jobName"), taskID, ctx.Logger)
	if err != nil {
		ctx.Err = err
		return
	}
	c.Writer.Header().Set("Content-Disposition", `attachment; filename="artifact.tar.gz"`)

	c.Data(200, "application/octet-stream", resp)
}

func GetWorkflowV4CodeLogs(ctx *gin.Context) {
	log.Errorf("GetWorkflowV4CodeLogs start")
	taskID := ctx.Param("taskID")
	workflowName := ctx.Param("workflowName")
	context := internalhandler.NewContext(ctx)
	defer func() { internalhandler.JSONResponse(ctx, context) }()

	gitDir, _ := getDir(taskID, workflowName, context)

	gitFetch(gitDir)
	files := gitLog(gitDir)
	s := ""
	for line := range files {
		s2 := files[line]
		if strings.Contains(s2, "<") {
			s2 = strings.Replace(s2, "<", "&lt", -1)
		}
		if strings.Contains(s2, ">") {
			s2 = strings.Replace(s2, "<", "&gt", -1)
		}
		if strings.Contains(s2, " ") {
			s2 = strings.Replace(s2, " ", "&nbsp;", -1)
		}

		if strings.HasPrefix(s2, "+") {
			s += fmt.Sprintln(`<tr><td bgcolor=#B0E0E6>` + s2 + `</td></tr>`)
		} else if strings.HasPrefix(s2, "-") {
			s += fmt.Sprintln(`<tr><td bgcolor=#FFC0CB>` + s2 + `</td></tr>`)
		} else {
			s += fmt.Sprintln(`<tr><td>` + s2 + `</td></tr>`)
		}

	}

	ctx.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	content := `
	<!DOCTYPE html>
				<html>
				<head>
				<title>Code Review</title>
				</head>
				<body>
				<table>
					<tbody>` + s + `</tbody>
				</table>
				</body>
				</html>`
	ctx.String(200, strings.Join(files, content))
}

func getDir(taskID string, workflowName string, context *internalhandler.Context) (string, string) {

	taskId, _ := strconv.ParseInt(taskID, 10, 64)
	v4, _ := workflow.GetWorkflowTaskV4(workflowName, taskId, context.Logger)

	gitDir := "/var/lib/workspace/"
	branch := ""
	cache := GetCache()
	if len(cache.Read("workDir_key"+workflowName+taskID)) > 0 {
		gitDir = cache.Read("workDir_key" + workflowName + taskID)
		branch = cache.Read("workBranch_key" + workflowName + taskID)
		return gitDir, branch
	}
	for _, stage := range v4.Stages {
		log.Info(stage)
		if strings.EqualFold(stage.Name, "代码审查") {
			for _, job := range stage.Jobs {
				log.Info(job)
				if strings.EqualFold(job.Name, "codereview") {
					log.Info("Spec", job.Spec)
					freestyleJobSpec := job.Spec.(workflow.ZadigBuildJobSpec)
					log.Info("freestyleJobSpec", freestyleJobSpec)
					for _, repos := range freestyleJobSpec.Repos {
						if strings.Contains(repos.Address, "116.196.73.141") {
							log.Info("repos", repos)
							_, err := service.GetGitRepoInfo(repos.CodehostID, repos.RepoOwner, repos.RepoNamespace, repos.RepoName, repos.Branch, repos.RemoteName, "", context.Logger)
							if err != nil {
								log.Errorf(err.Error())
							}
							gitDir = gitDir + repos.RepoName
							if strings.EqualFold(repos.RepoName, "zsj-zhaobiao") {
								gitDir = gitDir + "/zhaobiao-parent"
							} else if strings.EqualFold(repos.RepoName, "zsj-xunjia") {
								gitDir = gitDir + "/zsj-xunjia-root"
							}
							branch = repos.Branch
							cache.Write("workDir_key"+workflowName+taskID, gitDir)
							cache.Write("workBranch_key"+workflowName+taskID, branch)
							break
						}
					}

				}
			}
		}
	}
	return gitDir, branch
}

func gitLog(dir string) []string {
	command := exec.Command("git", "log", "-p", "--after=\"3 day ago\"")
	command.Dir = dir

	pipe, err := command.StdoutPipe()
	err = command.Start()
	if err != nil {
		log.Errorf("git log exec error,%s", err)
	}
	reader := bufio.NewReader(pipe)
	var result []string
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			log.Errorf("git log read error,%s", err)
			break
		}
		s := string(line)
		log.Info(s)
		result = append(result, s)
	}

	return result
}

type Author struct {
	Name  string `json:"name"`
	Id    string `json:"id"`
	GitId string `json:"gitId"`
}

func GetAuthors(ctx *gin.Context) {
	ctx.JSON(200, gin.H{"result": configAuth()})
}

func configAuth() []Author {
	authors := make([]Author, 0)
	authors = append(authors, Author{"宋文彬", "swb", "songwb@zhulong.com.cn"})
	authors = append(authors, Author{"吴小乔", "wxq", "wuxq@zhulong.con.cn"})
	authors = append(authors, Author{"倪枫", "nf", "nif@zhulong.com.cn"})
	authors = append(authors, Author{"廖楚波", "lcb", "liaocb@zhulong.com.cn"})
	authors = append(authors, Author{"赵志权", "zzq", "zhaozq@zhulong.com.cn8014"})
	authors = append(authors, Author{"肖梦银", "xmy", "xiaomy@zhulong.com.cn"})
	authors = append(authors, Author{"章瑶", "zy", "zhangyao@zhulong.com.cn"})
	return authors
}

func GetFileLogs(ctx *gin.Context) {
	file := ctx.Param("file")
	taskID := ctx.Param("taskID")
	workflowName := ctx.Param("workflowName")
	context := internalhandler.NewContext(ctx)
	defer func() { internalhandler.JSONResponse(ctx, context) }()
	cache := GetCache()
	file = cache.Read(file)
	log.Info(file)

	dir, branch := getDir(taskID, workflowName, context)
	gitFetch(dir)
	files := fileLog(dir, "remotes/origin/"+branch, file)

	writeCodeHtml(files, ctx)
}

func fileLog(dir string, branch string, file string) []FileLogInfo {
	logs := make([]FileLogInfo, 0)
	command := exec.Command("git", "blame", branch, file)
	command.Dir = dir

	pipe, err := command.StdoutPipe()
	err = command.Start()
	if err != nil {
		log.Errorf(err.Error())
	}

	reader := bufio.NewReader(pipe)

	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			log.Errorf(err.Error())
			break
		}
		s := string(line)

		hash := s[:10]
		author := strings.Split(strings.SplitAfter(s, "(")[1], " ")[0]
		code := strings.SplitAfter(s, ")")[1]
		time := strings.SplitAfter(strings.SplitAfter(s, author)[1], " +0800")[0]
		lin := strings.Split(strings.Split(s, "+0800")[1], ")")[0]
		logs = append(logs, FileLogInfo{hash, author, time, lin, code})
	}

	return logs
}

type FileLogInfo struct {
	Hash   string `json:"hash"`
	Author string `json:"author"`
	Time   string `json:"time"`
	Line   string `json:"line"`
	Code   string `json:"code"`
}

func writeCodeHtml(files []FileLogInfo, ctx *gin.Context) {
	s := ""
	for line := range files {
		info := files[line]
		s2 := info.Code
		if strings.Contains(s2, "<") {
			s2 = strings.Replace(s2, "<", "&lt", -1)
		}
		if strings.Contains(s2, ">") {
			s2 = strings.Replace(s2, "<", "&gt", -1)
		}
		if strings.Contains(s2, " ") {
			s2 = strings.ReplaceAll(s2, " ", "&nbsp;")
		}

		s += fmt.Sprintln(`<tr>` +
			//`<td width=2%>` + info.Hash + `</td>` +
			`<td width=200px>` + "&nbsp;&nbsp;" + info.Author + `&nbsp;&nbsp;</td>` +
			`<td width=180px>` + strings.ReplaceAll(info.Time, "+0800", "") + `&nbsp;&nbsp;</td>` +
			`<td width=20px class=` + info.Hash + `>` + info.Line + `&nbsp;&nbsp;</td>` +
			`<td class=` + info.Hash + `>` + info.Code + `&nbsp;&nbsp;</td>` +
			`</tr>`)

	}

	ctx.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	content := `
	<!DOCTYPE html>
				<html>
				<head>
				<title>Code Review</title>
				</head>
				<body>
				<table  border=0 cellspacing=0 width=100% cellspacing=10px>
					<tbody>` +
		s +
		`</tbody>
				</table>
				</body>
				</html>`
	log.Info(content)
	ctx.String(200, content)
	ctx.Writer.Flush()
	ctx.Writer.CloseNotify()
}

func GetHashLogs(ctx *gin.Context) {
	hash := ctx.Param("hash")
	workflowName := ctx.Param("workflowName")
	taskID := ctx.Param("taskID")
	context := internalhandler.NewContext(ctx)
	defer func() { internalhandler.JSONResponse(ctx, context) }()

	dir, _ := getDir(taskID, workflowName, context)
	gitFetch(dir)
	files := commitLog(dir, hash)
	writeHtml(files, ctx)
}

func commitLog(dir string, hash string) []string {

	command := exec.Command("git", "show", hash)
	command.Dir = dir

	pipe, err := command.StdoutPipe()
	err = command.Start()
	if err != nil {
		log.Error(err)
	}

	reader := bufio.NewReader(pipe)

	var result []string
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			log.Error(err)
			break
		}
		s := string(line)
		if strings.HasPrefix(s, "diff --git") {
			continue
		}
		result = append(result, s)
	}

	return result
}

func writeHtml(files []string, ctx *gin.Context) {
	s := ""
	for line := range files {
		s2 := files[line]
		if strings.Contains(s2, "<") {
			s2 = strings.Replace(s2, "<", "&lt", -1)
		}
		if strings.Contains(s2, ">") {
			s2 = strings.Replace(s2, "<", "&gt", -1)
		}
		if strings.Contains(s2, " ") {
			s2 = strings.ReplaceAll(s2, " ", "&nbsp;")
		}

		if strings.HasPrefix(s2, "+") {
			s += fmt.Sprintln(`<tr><td bgcolor=#B0E0E6>` + s2 + `</td></tr>`)
			if strings.Contains(s2, "+++") {
				s += fmt.Sprintln(`<tr><td>&nbsp;</td></tr>`)
			}
		} else if strings.HasPrefix(s2, "-") {
			if strings.Contains(s2, "---") {
				s += fmt.Sprintln(`<tr><td>&nbsp;</td></tr>`)
			}
			s += fmt.Sprintln(`<tr><td bgcolor=#FFC0CB>` + s2 + `</td></tr>`)
		} else {
			s += fmt.Sprintln(`<tr><td>` + s2 + `</td></tr>`)
		}

	}

	ctx.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	content := `
	<!DOCTYPE html>
				<html>
				<head>
				<title>Code Review</title>
				</head>
				<body>
				<table  border=0 cellspacing=0 width=100%>
					<tbody>` +
		s +
		`</tbody>
				</table>
				</body>
				</html>`

	log.Info(content)
	ctx.String(200, content)
	ctx.Writer.Flush()
	ctx.Writer.CloseNotify()
}

func GetDiffFiles(ctx *gin.Context) {
	author := ctx.Param("author")
	auth := configAuth()
	for _, au := range auth {
		if strings.EqualFold(author, au.Id) {
			author = au.GitId
		}
	}
	workflowName := ctx.Param("workflowName")
	taskID := ctx.Param("taskID")
	context := internalhandler.NewContext(ctx)
	defer func() { internalhandler.JSONResponse(ctx, context) }()

	dir, branch := getDir(taskID, workflowName, context)
	gitFetch(dir)
	files := getDiffFiles(dir, "remotes/origin/"+branch, author)

	ctx.JSON(200, gin.H{"result": files})
}

type FileInfo struct {
	Id       string `json:"id"`
	FileName string `json:"fileName"`
}

func getDiffFiles(dir string, branch string, author string) []FileInfo {
	files := make([]FileInfo, 0)
	command := exec.Command("git", "diff", branch+"..remotes/origin/master", "--author="+author, "--name-status")
	command.Dir = dir

	pipe, err := command.StdoutPipe()
	err = command.Start()
	if err != nil {
		log.Error(err)
	}

	reader := bufio.NewReader(pipe)

	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			log.Error(err)
			break
		}
		str := string(line)
		fileKey := uuid.Generate().String()
		files = append(files, FileInfo{fileKey, str[strings.Index(str, "/")+1:]})
		cache := GetCache()
		cache.Write(fileKey, str[strings.Index(str, "/")+1:])
	}

	return files
}

type MyCache struct {
	MyCache *bigcache.BigCache
}

var Cache *MyCache

func GetCache() *MyCache {
	if nil == Cache {
		NewBigCache()
	}
	return Cache
}
func NewBigCache() {
	bCache, err := bigcache.NewBigCache(bigcache.Config{
		// 分片数量 (必须是2的幂次方)
		Shards: 1024,

		// 存活时间，过了该时间才会删除元素
		LifeWindow: 7 * 24 * time.Hour,

		//删除过期元素的时间间隔(清理缓存).
		// 如果设置为<= 0，则不执行任何操作
		// 设置为< 1秒会适得其反— bigcache只能精确到1秒.
		CleanWindow: 5 * time.Minute,

		// rps * lifeWindow, 仅用于初始内存分配
		MaxEntriesInWindow: 1000 * 10 * 60,

		// 以字节为单位的元素大小最大值，仅在初始内存分配时使用
		MaxEntrySize: 500,

		// 打印内存分配信息
		Verbose: false,

		// 缓存分配的内存不会超过这个限制, MB单位
		// 如果达到值，则可以为新条目覆盖最旧的元素
		// 0值表示没有限制
		HardMaxCacheSize: 256,

		// 当最旧的元素由于过期时间或没有剩余空间而被删除时，触发回调
		// 对于新元素，或者因为调用了delete。将返回一个表示原因的位掩码.
		// 默认值为nil，这意味着没有回调.
		OnRemove: nil,

		// OnRemoveWithReason当因为过期时间或没有空间时，最老一条元素被删除会触发该回调。会返回删除原因。
		// 默认值为nil。
		OnRemoveWithReason: nil,
	})
	if err != nil {
		log.Error(err)
	}

	Cache = &MyCache{bCache}
}

func (bc *MyCache) Read(key string) string {
	bs, err := bc.MyCache.Get(key)
	if err != nil {
		return ""
	}

	return string(bs)
}

func (bc *MyCache) Write(key string, value string) {
	bc.MyCache.Set(key, []byte(value))
}

type MessageInfo struct {
	Msgtype string         `json:"msgtype"`
	Text    MessageContent `json:"text"`
}

type MessageContent struct {
	Content string `json:"content"`
}

func gitFetch(dir string) {
	log.Infof("fetch,s%", dir)
	command := exec.Command("git", "fetch", "origin")
	command.Dir = dir

	err := command.Run()
	if err != nil {
		log.Error(err)
	}
}

func GetCommits(ctx *gin.Context) {
	taskID := ctx.Param("taskID")
	workflowName := ctx.Param("workflowName")
	context := internalhandler.NewContext(ctx)
	defer func() { internalhandler.JSONResponse(ctx, context) }()

	gitDir, branch := getDir(taskID, workflowName, context)
	files := getCommits(gitDir, "remotes/origin/"+branch)

	ctx.JSON(200, gin.H{"result": files})
}

type BranchInfo struct {
	Author  string `json:"author"`
	Message string `json:"message"`
	Time    string `json:"time"`
	Hash    string `json:"hash"`
}

func getCommits(dir string, branch string) []BranchInfo {
	var commits []BranchInfo

	command := exec.Command("git", "log", branch, "--")
	command.Dir = dir
	pipe, err := command.StdoutPipe()
	err = command.Start()
	if err != nil {
		log.Error(err)
	}

	reader := bufio.NewReader(pipe)

	var br BranchInfo
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			log.Error(err)
			break
		}
		s := string(line)
		log.Info(s)
		if strings.Contains(s, "commit") {
			if len(br.Message) > 0 {
				commits = append(commits, br)
			}
			br = BranchInfo{}
			br.Hash = strings.TrimSpace(strings.Split(s, "commit")[1])
		}
		if strings.Contains(s, "Author") {
			s2 := strings.Split(s, "<")[1]
			br.Author = strings.TrimSpace(strings.Replace(s2, ">", "", -1))
		}
		if strings.Contains(s, "Date:") {
			br.Time = strings.TrimSpace(strings.Split(s, "Date: ")[1])
		}
		if strings.Contains(s, "   ") {
			space := strings.TrimSpace(strings.Split(s, "   ")[1])
			if len(br.Message) > 0 {
				br.Message = br.Message + ";" + space
			} else {
				br.Message = space
			}
		}
	}

	return commits
}
