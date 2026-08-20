// Package drivers 通过空导入聚合所有驱动，触发各驱动的 init() 注册。
package drivers

import (
	// 仅注册自维护网盘：115 盘（115_Open + 自建 OAuth 代理，AppID 授权）与天翼云盘（189Cloud，本地扫码直连）。
	// 其余驱动归档：代码保留在 drivers/ 下，需要时取消注释即可恢复注册。
	_ "litepan/drivers/115_Open"
	_ "litepan/drivers/189Cloud"
	// _ "litepan/drivers/123_Open"
	// _ "litepan/drivers/139Cloud"
	// _ "litepan/drivers/Baidu_Open"
	// _ "litepan/drivers/Guangya"
	// _ "litepan/drivers/OneDrive"
	// _ "litepan/drivers/OpenList"
	// _ "litepan/drivers/Quark"
	// _ "litepan/drivers/WebDAV"
)