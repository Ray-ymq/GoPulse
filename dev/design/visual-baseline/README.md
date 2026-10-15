# 视觉基线

本目录保存统一前端设计系统实施后的 HTML 样例基线，用于复核两种布局变体在主要视口下的层级、密度和响应式折叠。

- 样例：`admin-design-preview.html`、`user-design-preview.html`
- 视口：1440×900、1024×900、768×900、390×844
- 浏览器：Playwright Chromium headless，device scale factor 1
- 数据：样例文件内的静态正常、关注、失败、趋势和空状态数据
- 生成日期：2026-10-15

文件命名格式为 `{admin|user}-{desktop|tablet|compact|mobile}.png`。真实 Vue 页面仍需通过带后端依赖的 E2E 环境做运行态验收。
