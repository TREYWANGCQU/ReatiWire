// collaboration_tool_solution/team_collab/main.go
package main

import (
	"embed"
	"fmt"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	fmt.Println("================================================================================")
	fmt.Println("  ReatiWire: 20人轻量私有 IM 与在线会议协同系统 (Dedicated Standalone GUI Client)")
	fmt.Println("================================================================================")

	app := NewApp()

	// 纯独立 GUI 桌面应用入口 (Wails v2 + Evergreen WebView2)
	// 物理抹除宿主机 TCP 环回控制端口 (34115)，内存直接映射 API 与前端资产
	err := wails.Run(&options.App{
		Title:             "ReatiWire - 私有 IM 与协同工作台",
		Width:             1280,
		Height:            850,
		MinWidth:          1024,
		MinHeight:         700,
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.CreateAPIMux(),
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 255},
		OnStartup:        app.Startup,
		OnShutdown:       app.Shutdown,
		Bind: []interface{}{
			app,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "reati-wire-app-v1-lock",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				if app.ctx != nil {
					runtime.WindowShow(app.ctx)
					runtime.WindowUnminimise(app.ctx)
				}
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	})

	if err != nil {
		log.Fatalf("[-] Wails 客户端启动异常: %v\n", err)
	}
}
