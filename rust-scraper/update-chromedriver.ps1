# PowerShell script to download and install the correct ChromeDriver version
# This script detects your Chrome version and downloads the matching ChromeDriver

Write-Host "ChromeDriver Update Script" -ForegroundColor Green
Write-Host "===========================" -ForegroundColor Green

# Detect Chrome version
$chromePath = "C:\Program Files\Google\Chrome\Application\chrome.exe"
if (Test-Path $chromePath) {
    $chromeVersion = (Get-Item $chromePath).VersionInfo.FileVersion
    Write-Host "Detected Chrome version: $chromeVersion" -ForegroundColor Cyan
} else {
    Write-Host "Chrome not found at $chromePath" -ForegroundColor Red
    exit 1
}

# Extract major version (e.g., 141 from 141.0.7390.55)
$majorVersion = $chromeVersion.Split('.')[0]
Write-Host "Chrome major version: $majorVersion" -ForegroundColor Cyan

# Get the matching ChromeDriver version
Write-Host "Fetching matching ChromeDriver version..." -ForegroundColor Yellow
$apiUrl = "https://googlechromelabs.github.io/chrome-for-testing/latest-versions-per-milestone-with-downloads.json"

try {
    $response = Invoke-RestMethod -Uri $apiUrl -Method Get
    $milestone = $response.milestones.$majorVersion

    if (-not $milestone) {
        Write-Host "No matching ChromeDriver found for Chrome version $majorVersion" -ForegroundColor Red
        Write-Host "Please manually download from: https://googlechromelabs.github.io/chrome-for-testing/" -ForegroundColor Yellow
        exit 1
    }

    $chromedriverVersion = $milestone.version
    Write-Host "Found matching ChromeDriver version: $chromedriverVersion" -ForegroundColor Green

    # Find Windows 64-bit download URL
    $downloadUrl = $null
    foreach ($download in $milestone.downloads.chromedriver) {
        if ($download.platform -eq "win64") {
            $downloadUrl = $download.url
            break
        }
    }

    if (-not $downloadUrl) {
        Write-Host "No Windows 64-bit ChromeDriver download found" -ForegroundColor Red
        exit 1
    }

    Write-Host "Download URL: $downloadUrl" -ForegroundColor Cyan

    # Create temp directory for download
    $tempDir = Join-Path $env:TEMP "chromedriver_temp"
    if (Test-Path $tempDir) {
        Remove-Item -Recurse -Force $tempDir
    }
    New-Item -ItemType Directory -Path $tempDir | Out-Null

    $zipPath = Join-Path $tempDir "chromedriver.zip"

    # Download ChromeDriver
    Write-Host "Downloading ChromeDriver..." -ForegroundColor Yellow
    Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath

    # Extract ChromeDriver
    Write-Host "Extracting ChromeDriver..." -ForegroundColor Yellow
    Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force

    # Find the chromedriver.exe in the extracted files
    $chromedriverExe = Get-ChildItem -Path $tempDir -Recurse -Filter "chromedriver.exe" | Select-Object -First 1

    if (-not $chromedriverExe) {
        Write-Host "Could not find chromedriver.exe in extracted files" -ForegroundColor Red
        exit 1
    }

    # Copy to rust-scraper directory
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    $destPath = Join-Path $scriptDir "chromedriver.exe"

    # Stop any running ChromeDriver processes
    Write-Host "Stopping any running ChromeDriver processes..." -ForegroundColor Yellow
    Get-Process -Name "chromedriver" -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2

    Copy-Item -Path $chromedriverExe.FullName -Destination $destPath -Force

    # Clean up
    Remove-Item -Recurse -Force $tempDir

    Write-Host "`nChromeDriver successfully updated!" -ForegroundColor Green
    Write-Host "Location: $destPath" -ForegroundColor Cyan
    Write-Host "Version: $chromedriverVersion" -ForegroundColor Cyan
    Write-Host "`nYou can now restart your Rust scraper service." -ForegroundColor Green

} catch {
    Write-Host "Error: $_" -ForegroundColor Red
    Write-Host "Please manually download ChromeDriver from: https://googlechromelabs.github.io/chrome-for-testing/" -ForegroundColor Yellow
    exit 1
}
