package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/data"
)

func main() {
	var (
		exchange  = flag.String("exchange", "binance", "交易所名称 (binance)")
		symbols   = flag.String("symbols", "BTCUSDT", "交易对，多个用逗号分隔")
		intervals = flag.String("intervals", "1h", "时间周期，多个用逗号分隔")
		startDate = flag.String("start", "", "开始日期 YYYY-MM-DD")
		endDate   = flag.String("end", "", "结束日期 YYYY-MM-DD (默认今天)")
		workers   = flag.Int("workers", 4, "并发下载数")
		wait      = flag.Bool("wait", true, "是否等待下载完成")
	)
	flag.Parse()

	if *startDate == "" {
		log.Fatal("必须指定 --start 日期，格式 YYYY-MM-DD")
	}
	if *endDate == "" {
		*endDate = time.Now().Format("2006-01-02")
	}

	store := data.NewStorage()
	if store == nil {
		log.Fatal("数据库未初始化，请先启动 gateway 或设置 DB_PATH 环境变量")
	}

	downloader := data.NewDownloader(store)
	cfg := data.DownloadConfig{
		Exchange:   *exchange,
		Symbols:    strings.Split(*symbols, ","),
		Intervals:  strings.Split(*intervals, ","),
		StartDate:  *startDate,
		EndDate:    *endDate,
		MaxWorkers: *workers,
	}

	jobID, err := downloader.StartDownload(cfg)
	if err != nil {
		log.Fatalf("启动下载失败: %v", err)
	}

	fmt.Printf("下载任务已启动: %s\n", jobID)

	if !*wait {
		os.Exit(0)
	}

	fmt.Println("等待下载完成...")
	for {
		job := downloader.GetJob(jobID)
		if job == nil {
			log.Fatal("任务不存在")
		}
		fmt.Printf("\r状态: %s | 进度: %d/%d", job.Status, job.Progress, job.Total)
		if job.Status == "done" {
			fmt.Println("\n下载完成")
			break
		}
		if job.Status == "failed" {
			fmt.Printf("\n下载失败: %s\n", job.Error)
			os.Exit(1)
		}
		time.Sleep(1 * time.Second)
	}
}
