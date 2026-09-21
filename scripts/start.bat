@echo off
REM ============================================================================
REM FM TUL Discord Connector – Start Script (Windows)
REM ============================================================================

setlocal enabledelayedexpansion

set SCRIPT_DIR=%~dp0
set PROJECT_DIR=%SCRIPT_DIR%..
set BINARY_NAME=fm-tul-bot.exe

cd /d "%PROJECT_DIR%"

echo ======================================================
echo    FM TUL - Discord Connector
echo ======================================================
echo.

REM --- 1. Kontrola Go ---
where go >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Go neni nainstalovano!
    echo    Nainstalujte Go z: https://go.dev/dl/
    pause
    exit /b 1
)
for /f "tokens=3" %%v in ('go version') do set GO_VERSION=%%v
echo [OK] Go nalezeno: %GO_VERSION%

REM --- 2. Kontrola .env ---
if not exist ".env" (
    if exist ".env.example" (
        echo [WARN] .env soubor nenalezen. Kopiruji z .env.example...
        copy .env.example .env >nul
        echo    Prosim upravte .env a vyplnte konfiguraci!
        echo.
    ) else (
        echo [ERROR] .env ani .env.example nenalezeny!
        pause
        exit /b 1
    )
)
echo [OK] Konfigurace .env nalezena

REM --- 3. Stazeni zavislosti ---
echo Stahuji zavislosti...
go mod tidy
if errorlevel 1 (
    echo [ERROR] Chyba pri stahovani zavislosti!
    pause
    exit /b 1
)
echo [OK] Zavislosti pripraveny

REM --- 4. Kompilace ---
echo Kompiluji %BINARY_NAME%...
go build -buildvcs=false -o %BINARY_NAME% .\cmd\bot\
if errorlevel 1 (
    echo [ERROR] Chyba pri kompilaci!
    pause
    exit /b 1
)
echo [OK] Kompilace uspesna: %BINARY_NAME%

REM --- 5. Spusteni ---
echo.
echo Spoustim FM TUL Discord Connector...
echo ──────────────────────────────────────────────
%BINARY_NAME%

pause
