package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"spc/internal/core"
	"spc/internal/generator"
	"spc/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	step := flag.Duration("step", 5*time.Minute, "每秒推进的模拟时长")
	seed := flag.Int64("seed", 42, "随机种子")
	flag.Parse()

	eng := core.NewEngine()
	gen := generator.New(eng, *seed, *step)
	gen.ScheduleEvents()
	gen.Start()

	srv := server.New(eng, gen)
	log.Printf("SPC 系统已启动: http://localhost%s (模拟时钟每秒钟推进 %v)", *addr, *step)
	log.Fatal(http.ListenAndServe(*addr, srv))
}
