@echo off
rem Double-click to build Carta Desktop from this folder and open it. For
rem everyday use, the released Carta.exe needs no build at all; this is for
rem running the code as it is here. Surveys are saved in dist\carta-surveys,
rem next to the program (gitignored).
setlocal
title Carta
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo Go is not installed, or not on PATH. Install it from https://go.dev/dl/
  goto fail
)

rem The desktop app draws with OpenGL through cgo, so it needs a C compiler.
rem MSYS2's gcc is the one Fyne recommends on Windows.
where gcc >nul 2>nul
if errorlevel 1 if exist "C:\msys64\ucrt64\bin\gcc.exe" set "PATH=C:\msys64\ucrt64\bin;%PATH%"
where gcc >nul 2>nul
if errorlevel 1 (
  echo A C compiler is needed to build the desktop app. Install MSYS2 from https://www.msys2.org/
  echo then, in its terminal: pacman -S mingw-w64-ucrt-x86_64-gcc
  goto fail
)

echo Building Carta Desktop. The first build takes several minutes; later ones take seconds.
set CGO_ENABLED=1
pushd desktop
go build -trimpath -ldflags "-s -w -H windowsgui" -o ..\dist\carta-desktop.exe .\cmd\carta-desktop
set built=%errorlevel%
popd
if not "%built%"=="0" (
  echo.
  echo The build failed. The errors are above.
  goto fail
)

start "" "dist\carta-desktop.exe" %*
exit /b 0

:fail
echo.
pause
exit /b 1
