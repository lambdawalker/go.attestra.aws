@echo off
setlocal
pushd "%~dp0" || exit /b 1
go run -buildvcs=false ./cmd/deploy %*
set "deploy_exit=%ERRORLEVEL%"
popd
exit /b %deploy_exit%
