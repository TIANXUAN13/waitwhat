@echo off
setlocal

echo ===================================================
echo   WaitWhat Full Stack Launcher (Windows)
echo ===================================================
echo.

:: 1. Check for Go
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Go is not installed. Backend cannot start.
    pause
    exit /b 1
)

:: 2. Check for Node.js
node -v >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Node.js is not installed. Frontend cannot start.
    pause
    exit /b 1
)

:: 3. Setup Directories
set "ROOT_DIR=%~dp0"
set "APP_DATA_DIR=%ROOT_DIR%data"
if not exist "%APP_DATA_DIR%" mkdir "%APP_DATA_DIR%"

:: 4. Start Backend
echo [1/2] Starting backend on port 18080...
:: Run go run from ROOT instead of inside backend folder to keep ./data paths consistent
set "BACKEND_CMD=set APP_PORT=18080 && set APP_DATA_DIR=./data && set APP_CORS_ALLOW_ORIGIN=http://localhost:5173,http://127.0.0.1:5173 && go run ./backend"
start "WaitWhat Backend (18080)" cmd /c "%BACKEND_CMD%"

:: 5. Start Frontend
echo [2/2] Starting frontend on port 5173...
:: Check if node_modules exists
if not exist "%ROOT_DIR%frontend\node_modules" (
    echo [INFO] node_modules not found, running npm install...
    cd /d "%ROOT_DIR%frontend" && call npm install
)

set "FRONTEND_CMD=cd /d %ROOT_DIR%frontend && set VITE_API_BASE=http://localhost:18080/api && npm run dev"
start "WaitWhat Frontend (5173)" cmd /c "%FRONTEND_CMD%"

echo.
echo ===================================================
echo   WaitWhat is starting up!
echo.
echo   - Backend: http://localhost:18080/api
echo   - Frontend: http://localhost:5173
echo.
echo   Please wait for the new windows to load...
echo ===================================================
echo.
pause
