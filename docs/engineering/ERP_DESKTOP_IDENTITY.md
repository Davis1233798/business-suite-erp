# ERP Windows 桌面身分與資料邊界

## 架構決策

ERP Windows Electron 套件使用 `org.blockchen.forgeflow.erp` 作為作業系統 appId，產品名稱為 `ForgeFlow ERP`。啟動時將 Electron `userData` 固定至 `%APPDATA%\ForgeFlow ERP`，Go ERP repository 資料庫固定使用該目錄內的 `erp-repository.sqlite`。路徑由純函式集中產生，避免 app metadata、Electron 路徑與 Go 環境變數各自命名。

桌面套件仍只附帶本站 Go backend 與 React assets，Go 仍監聽隨機 loopback port。此改動不新增資料搬移、共用資料庫、加密、雲端同步或正式備份能力；既有 `Business Suite` userData 不會被自動讀取或搬移。

## 邊界與驗收

- appId、產品名稱與 Windows 使用者資料夾均為 ERP 專屬值。
- 資料庫位於 ERP userData 內，檔名明確標示 ERP；不可回退到舊共用 `suite.sqlite`。
- 相對或非字串 appData 路徑必須拒絕，避免意外落入工作目錄。
- 啟動 Go backend 前先設定 ERP userData；啟動命令的 `SUITE_DB` 必須使用同一份集中路徑。
- Windows CI 執行 identity 邊界測試、建置 web/Go assets、封裝未安裝的 Windows 套件，並上傳可檢視 artifact。PR 的 CI 結果才是建置驗收；不以未執行的本機測試宣稱通過。
