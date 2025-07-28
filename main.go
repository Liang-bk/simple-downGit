package main

import (
	"container/list"
	"errors"
	"flag"
	"fmt"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	downloadUrl string
	output      string
)

// parseArgs
// downloadUrl: 文件或文件夹对应的github网址
// output: 文件保存的本地路径, 默认为exe同目录下的download文件夹中
func parseArgs() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Error getting executable path: ", exePath)
		os.Exit(1)
	}
	exeDir := filepath.Dir(exePath)
	githubUrl := flag.String("url", "", "file's github (required)")
	outputPath := flag.String("output", filepath.Join(exeDir, "download"), "directory to save the file")

	flag.Parse()
	downloadUrl = *githubUrl
	output = *outputPath

	if downloadUrl == "" {
		fmt.Println("Error: Missing required flag: -")
		fmt.Println("Usage:")
		flag.PrintDefaults()
		os.Exit(1)
	}
}

// transformGithubURL: 将github网址转为对应的文件网址
func transformGithubURL(githubUrl string) (string, error) {
	if !strings.Contains(githubUrl, "github.com") {
		return "", errors.New("invalid Github URL: " + githubUrl)
	}
	// github.com -> raw.githubusercontent.com
	replaceUrl := strings.Replace(githubUrl, "github.com", "raw.githubusercontent.com", 1)
	// /blob/ -> /
	replaceUrl = strings.Replace(replaceUrl, "/blob/", "/", 1)
	_, err := url.Parse(replaceUrl)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s, error: %v", replaceUrl, err)
	}
	return replaceUrl, nil
}

func initSetting() {
	// 进度条
	// TODO: 代理
	// goroutine多协程请求
	// TODO: github api配置
	// TODO: 暂停/续传?
}

func main() {
	parseArgs()
	initSetting()

	// 判断类型, 文件还是文件夹
	urlType, err := checkGithubPathType(downloadUrl)
	if err != nil {
		fmt.Errorf("check github url [%s] error: %v", downloadUrl, err)
		os.Exit(1)
	}
	if urlType == DIR {
		downloadDir(downloadUrl)
	} else {
		// 处理文件流程
		p := mpb.New(mpb.WithWidth(64))
		var wg sync.WaitGroup
		wg.Add(1)
		fileName := getNameFromURL(downloadUrl)
		bar := p.New(-1, mpb.BarStyle().Lbound("[").Rbound("]"),
			mpb.PrependDecorators(
				decor.Name(fileName, decor.WC{W: len(fileName) + 1, C: decor.DindentRight}),
				decor.CountersKibiByte("% .2f / % .2f"),
			),
			mpb.AppendDecorators(
				decor.OnAbort(decor.Percentage(decor.WC{W: 6}), "error!"),
				decor.OnComplete(decor.Percentage(decor.WC{W: 5}), "done!"),
			))
		go downloadFile(bar, &wg, downloadUrl, "")
		p.Wait()
	}
	fmt.Println("all files download done!")
}

func savedPathExist(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

// [url]
// url is a dir -> mkdir url, then solve [url/dir1, url/dir2, ... url/file]
// url is a file -> download it

func downloadFile(bar *mpb.Bar, wg *sync.WaitGroup, fileURL, savedPath string) {
	defer wg.Done()
	fileURL, err := transformGithubURL(fileURL)
	if err != nil {
		bar.Abort(false)
		errLog := fmt.Sprintf("Error transforming URL: %v\n", err)
		// 全局的log消息日志
		log.Println(errLog)
		return
	}

	resp, err := http.Get(fileURL)
	defer resp.Body.Close()
	if err != nil {
		bar.Abort(false)
		errLog := fmt.Sprintf("Error making http request for %s: %v\n", fileURL, err)
		log.Println(errLog)
		return
	}

	if resp.StatusCode != http.StatusOK {
		bar.Abort(false)
		errLog := fmt.Sprintf("Error downloading %s: bad status %s\n", fileURL, resp.Status)
		log.Println(errLog)
		return
	}
	// status is 200 ok
	var filePath string = savedPath
	var fileName string = getNameFromURL(fileURL)
	if filePath == "" {
		filePath = filepath.Join(output, fileName)
	}
	filePath = filepath.Join(output, filePath)
	var dirPath = filePath[:len(filePath)-len(fileName)]

	if !savedPathExist(dirPath) {
		err := os.MkdirAll(dirPath, os.ModePerm)
		if err != nil {
			bar.Abort(false)
			errLog := fmt.Sprintf("Error creating output directory: %v\n", err)
			log.Println(errLog)
			return
		}
	}

	file, err := os.Create(filePath)
	defer file.Close()
	if err != nil {
		bar.Abort(false)
		errLog := fmt.Sprintf("Error creating file %s: %v\n", filePath, err)
		log.Println(errLog)
		return
	}
	// 计算大小
	contentLen := resp.ContentLength
	if contentLen <= 0 {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			bar.Abort(false)
			errLog := fmt.Sprintf("Error reading response body for %s: %v\n", fileName, err)
			log.Println(errLog)
			return
		}
		contentLen = int64(len(data))
	}
	bar.SetTotal(contentLen, false)
	proxyReader := bar.ProxyReader(resp.Body)
	defer proxyReader.Close()

	_, err = io.Copy(file, proxyReader)

	if err != nil {
		// Abort the bar so p.Wait() doesn't hang.
		bar.Abort(false)
		errLog := fmt.Sprintf("Error writing file %s: %v\n", fileName, err)
		log.Println(errLog)
		return
	}
	// 确保进度条完成
	if !bar.Completed() {
		bar.SetTotal(contentLen, true)
	}
}

func downloadDir(dirURL string) {
	var dirName string = getNameFromURL(downloadUrl)

	var wg sync.WaitGroup
	// Create a progress container with a WaitGroup
	p := mpb.New(mpb.WithWaitGroup(&wg), mpb.WithWidth(60))

	// Use a buffered channel to limit concurrency
	maxConcurrency := 5
	guard := make(chan struct{}, maxConcurrency)

	// 进入这个文件夹, list所有文件夹/文件
	// 保存github url, 相对路径dir_path
	queue := list.New()
	queue.PushBack(GithubEntry{
		Name: dirName,
		Type: DIR,
		URL:  dirURL,
	})
	for queue.Len() > 0 {
		item := queue.Front()
		queue.Remove(item)
		if entry, ok := item.Value.(GithubEntry); ok {
			if entry.Type == DIR {
				entries, err := listGithubDirContents(entry.URL)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error listing github dir contents: %v\n", err)
					return
				}
				for _, elem := range entries {
					elem.Name = filepath.Join(dirName, elem.Name)
					queue.PushBack(elem)
				}
			} else {
				fileName := getNameFromURL(entry.URL)
				bar := p.New(-1, mpb.BarStyle().Lbound("[").Rbound("]"),
					mpb.PrependDecorators(
						decor.Name(fileName, decor.WC{W: len(fileName) + 1, C: decor.DindentRight}),
						decor.CountersKibiByte("% .2f / % .2f"),
					),
					mpb.AppendDecorators(
						decor.OnAbort(decor.Percentage(decor.WC{W: 7}), " error"),
						decor.OnComplete(decor.Percentage(decor.WC{W: 6}), " done!"),
					),
				)
				// Acquire a slot from the guard channel
				guard <- struct{}{}
				wg.Add(1)
				go func(entry GithubEntry) {
					defer func() { <-guard }() // Release the slot
					downloadFile(bar, &wg, entry.URL, entry.Name)
				}(entry)
			}
		}
	}

	// wg.Wait() // not necessary
	// Wait for all bars to complete
	p.Wait()
	fmt.Println("out of the queue")
}
