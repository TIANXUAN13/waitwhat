@echo off
setlocal DisableDelayedExpansion

:: Check for Go
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Go is not installed or not in PATH.
    pause
    exit /b 1
)

echo ===================================================
echo   WaitWhat Reminder Debug Tool (Windows)
echo ===================================================
echo.

:: 1. Build Backend
echo [1/4] Building backend...
cd backend
go build -o waitwhat_debug.exe .
if %errorlevel% neq 0 (
    echo [ERROR] Backend build failed.
    pause
    exit /b 1
)
cd ..

:: 2. Start Backend in a separate window
echo [2/4] Starting backend in a new window...
set APP_PORT=18080
set APP_DATA_DIR=./debug_data
if not exist "%APP_DATA_DIR%" mkdir "%APP_DATA_DIR%"
start "WaitWhat Backend" cmd /c "cd backend && set APP_PORT=18080 && set APP_DATA_DIR=../debug_data && waitwhat_debug.exe"

echo Waiting for backend to start (15s)...
timeout /t 15 /nobreak > nul

:: 3. Create a temporary PowerShell script line by line (Safest way in Batch)
echo [3/4] Running automated test sequence...
set "PS_SCRIPT=%TEMP%\waitwhat_test_script.ps1"

echo $ErrorActionPreference = 'Stop' > "%PS_SCRIPT%"
echo try { >> "%PS_SCRIPT%"
echo     $baseUrl = 'http://127.0.0.1:18080/api' >> "%PS_SCRIPT%"
echo     Write-Host '--- Initializing Database ---' >> "%PS_SCRIPT%"
echo     Invoke-RestMethod -Method Post -Uri "$baseUrl/database/init" -ContentType 'application/json' -Body '{"driver":"sqlite","sqlitePath":"./data/waitwhat.sqlite"}' ^| Out-Null >> "%PS_SCRIPT%"
echo     Write-Host '--- Setting up Admin ---' >> "%PS_SCRIPT%"
echo     $adminRes = Invoke-RestMethod -Method Post -Uri "$baseUrl/auth/setup-admin" -ContentType 'application/json' -Body '{"username":"admin","password":"password","email":"test@example.com","name":"Admin"}' >> "%PS_SCRIPT%"
echo     $token = $adminRes.token >> "%PS_SCRIPT%"
echo     $headers = @{ 'Authorization' = "Bearer $token"; 'Content-Type' = 'application/json' } >> "%PS_SCRIPT%"
echo     Write-Host '--- Configuring Mock SMTP ---' >> "%PS_SCRIPT%"
echo     Invoke-RestMethod -Method Post -Uri "$baseUrl/mail/config" -Headers $headers -Body '{"enabled":true,"host":"smtp.example.com","port":587,"username":"test","password":"pass","fromName":"WaitWhat","fromAddress":"test@example.com","useTls":true}' ^| Out-Null >> "%PS_SCRIPT%"
echo     Write-Host '--- Creating Notification Group ---' >> "%PS_SCRIPT%"
echo     $groupRes = Invoke-RestMethod -Method Post -Uri "$baseUrl/notify-groups" -Headers $headers -Body '{"name":"Test Group","enabled":true,"members":[@{"type":"email","label":"Test Email","target":"test@example.com","enabled":true}]}' >> "%PS_SCRIPT%"
echo     $groupId = $groupRes.group.id >> "%PS_SCRIPT%"
echo     Write-Host "--- Creating Event (Due in 1 min, but reminder 5 min before -> Already Due) ---" >> "%PS_SCRIPT%"
echo     $eventAt = [DateTime]::Now.AddMinutes(1).ToString('yyyy-MM-ddTHH:mm:sszzz') >> "%PS_SCRIPT%"
echo     $bodyObj = @{ >> "%PS_SCRIPT%"
echo         title = 'Debug Event' >> "%PS_SCRIPT%"
echo         content = 'Debug content' >> "%PS_SCRIPT%"
echo         eventAt = $eventAt >> "%PS_SCRIPT%"
echo         reminderEnabled = $true >> "%PS_SCRIPT%"
echo         recurrenceType = 'once' >> "%PS_SCRIPT%"
echo         reminderPoints = @(@{label='Early'; offsetMin=5}) >> "%PS_SCRIPT%"
echo         boundGroupIds = @($groupId) >> "%PS_SCRIPT%"
echo     } >> "%PS_SCRIPT%"
echo     $eventRes = Invoke-RestMethod -Method Post -Uri "$baseUrl/events" -Headers $headers -Body ($bodyObj ^| ConvertTo-Json) >> "%PS_SCRIPT%"
echo     Write-Host '--- Triggering Reminder Dispatch ---' >> "%PS_SCRIPT%"
echo     $dispatchRes = Invoke-RestMethod -Method Post -Uri "$baseUrl/reminders/dispatch" -Headers $headers >> "%PS_SCRIPT%"
echo     $dispatchRes ^| ConvertTo-Json -Depth 4 >> "%PS_SCRIPT%"
echo     Write-Host '--- Success: Check backend window for logs or check ./debug_data/logs/mail.log ---' >> "%PS_SCRIPT%"
echo } catch { >> "%PS_SCRIPT%"
echo     Write-Host "[ERROR] $($_.Exception.Message)" >> "%PS_SCRIPT%"
echo     if ($_.Exception.InnerException) { Write-Host "[INNER] $($_.Exception.InnerException.Message)" } >> "%PS_SCRIPT%"
echo     exit 1 >> "%PS_SCRIPT%"
echo } >> "%PS_SCRIPT%"

powershell -NoProfile -ExecutionPolicy Bypass -File "%PS_SCRIPT%"
set PS_EXIT=%ERRORLEVEL%
del "%PS_SCRIPT%"

if %PS_EXIT% neq 0 (
    echo.
    echo [ERROR] Test sequence failed.
) else (
    echo.
    echo [4/4] Debugging complete.
)

echo.
echo To stop the backend, run: taskkill /f /im waitwhat_debug.exe
pause
