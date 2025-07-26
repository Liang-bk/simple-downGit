package main

import (
	"container/list"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	// 代理
	// goroutine多协程请求
	// github api配置
	// 暂停/续传?
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
		downloadFile(downloadUrl, "")
	}
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

func downloadFile(fileURL, savedPath string) {
	fileURL, err := transformGithubURL(fileURL)
	if err != nil {
		return
	}
	resp, err := http.Get(fileURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error making http request: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: receive bad status code %s\n", resp.Status)
		return
	}
	var filePath, fileName string = savedPath, ""
	splits := strings.Split(fileURL, "/")
	fileName = splits[len(splits)-1]
	if filePath == "" {
		filePath = filepath.Join(output, fileName)
	}
	filePath = filepath.Join(output, filePath)
	var dirPath = filePath[:len(filePath)-len(fileName)]
	// 保存路径不存在, 先创建
	if !savedPathExist(dirPath) {
		err := os.MkdirAll(dirPath, os.ModePerm)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
			return
		}
	}
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
		return
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		return
	}
}

func downloadDir(dirURL string) {
	var dirName string = ""
	{
		splits := strings.Split(downloadUrl, "/")
		// dirName
		dirName = splits[len(splits)-1]
	}
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
				downloadFile(entry.URL, entry.Name)
			}
		}
	}
}
