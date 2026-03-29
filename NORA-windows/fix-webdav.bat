@echo off
echo Fixing Windows WebDAV settings (requires admin)...
echo.

:: File size limit: 4GB (default 50MB)
reg add "HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" /v FileSizeLimitInBytes /t REG_DWORD /d 4294967295 /f

:: PROPFIND response size limit: 200MB (default 1MB) — needed for large directories
reg add "HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" /v FileAttributesLimitInBytes /t REG_DWORD /d 209715200 /f

:: Allow HTTP WebDAV (not just HTTPS)
reg add "HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" /v BasicAuthLevel /t REG_DWORD /d 2 /f

:: 10 minute timeouts (default 30s)
reg add "HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" /v SendTimeout /t REG_DWORD /d 600000 /f
reg add "HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" /v ReceiveTimeout /t REG_DWORD /d 600000 /f

echo.
echo Restarting WebClient service...
net stop WebClient
net start WebClient

echo.
echo Done! Settings applied:
echo   - File size limit: 4GB
echo   - PROPFIND response limit: 200MB
echo   - Timeouts: 10 minutes
echo   - HTTP WebDAV: enabled
echo.
echo You can now run NORA normally (without admin).
pause
