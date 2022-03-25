package main

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

var wg sync.WaitGroup

var nextTask func()

func main() {
	logF, err := os.OpenFile("log.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetOutput(io.MultiWriter(os.Stderr, logF))
	f, err := os.Open("urls.txt")
	if err != nil {
		panic(err)
	}
	bf := bufio.NewScanner(f)
	nextTask = func() {
		for bf.Scan() {
			url := bf.Text()
			if len(url) < 32 || url[0] == '#' {
				continue
			}
			name := url[32 : len(url)-5]
			_, err := os.Stat(name)
			if err == nil {
				_, err = os.Stat(name + ".stat")
				if err != nil && os.IsNotExist(err) {
					log.Println(name, "已下载，跳过")
					continue
				}
			}
			curTask = &DownloadTask{webUrl: url}
			return
		}
		curTask = nil
		log.Println("任务分配结束，等待退出")
	}
	nextTask()
	runProxys("ips.txt")
	wg.Wait()

}

func getFileURL(url string) string {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", UA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	i := bytes.Index(body, ([]byte)("// is open ref count\nadd_ref"))
	if i == -1 {
		log.Println("failed to split fileid", url)
		return ""
	}
	body = body[i+29-31:]
	copy(body, "action=load_down_addr1&file_id=")
	i = bytes.IndexByte(body, ')')
	body = body[:i]
	req, _ = http.NewRequest("POST", "https://rosefile.net/ajax.php", bytes.NewReader(body))
	req.Header.Set("Referer", url)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//req.Header.Set("User-Agent", UA)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	body, err = io.ReadAll(resp.Body)
	i = bytes.IndexByte(body, '"')
	if i == -1 {
		log.Println("failed to split fileurl", url)
		return ""
	}
	body = body[i+1:]
	i = bytes.IndexByte(body, '"')

	return string(body[:i])
}
