# PowerShell script to install Chrome and matching ChromeDriver for the Rust scraper
# Run this script as Administrator on the Windows machine

param(
    [switch]$Force,
    [switch]$Headless
)

$ErrorActionPreference = "Stop"

# Get script directory at the start (before any functions)
$ScriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Definition }
if (-not $ScriptDir) { $ScriptDir = Get-Location }

Write-Host "=========================================" -ForegroundColor Green
Write-Host "  Rust Scraper - Chrome Setup Script    " -ForegroundColor Green
Write-Host "=========================================" -ForegroundColor Green
Write-Host ""
Write-Host "Script directory: $ScriptDir" -ForegroundColor Gray
Write-Host ""

# Check if running as Administrator
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "WARNING: This script should be run as Administrator for Chrome installation." -ForegroundColor Yellow
    Write-Host "Some features may not work without admin privileges." -ForegroundColor Yellow
    Write-Host ""
}

# Function to check if Chrome is installed
function Test-ChromeInstalled {
    $chromePaths = @(
        "C:\Program Files\Google\Chrome\Application\chrome.exe",
        "C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
        "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe"
    )

    foreach ($path in $chromePaths) {
        if (Test-Path $path) {
            return $path
        }
    }
    return $null
}

# Function to get Chrome version
function Get-ChromeVersion {
    param([string]$ChromePath)

    if (Test-Path $ChromePath) {
        return (Get-Item $ChromePath).VersionInfo.FileVersion
    }
    return $null
}

# Function to install Chrome
function Install-Chrome {
    Write-Host "Installing Google Chrome..." -ForegroundColor Yellow

    $tempDir = Join-Path $env:TEMP "chrome_installer"
    if (-not (Test-Path $tempDir)) {
        New-Item -ItemType Directory -Path $tempDir | Out-Null
    }

    $installerPath = Join-Path $tempDir "ChromeSetup.exe"
    $downloadUrl = "https://dl.google.com/chrome/install/latest/chrome_installer.exe"

    Write-Host "Downloading Chrome installer..." -ForegroundColor Cyan
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri $downloadUrl -OutFile $installerPath -UseBasicParsing
    } catch {
        Write-Host "Failed to download Chrome installer: $_" -ForegroundColor Red
        Write-Host "Please install Chrome manually from: https://www.google.com/chrome/" -ForegroundColor Yellow
        return $false
    }

    Write-Host "Running Chrome installer (this may take a few minutes)..." -ForegroundColor Cyan
    try {
        $process = Start-Process -FilePath $installerPath -ArgumentList "/silent /install" -Wait -PassThru
        if ($process.ExitCode -ne 0) {
            Write-Host "Chrome installer exited with code: $($process.ExitCode)" -ForegroundColor Yellow
        }
    } catch {
        Write-Host "Failed to run Chrome installer: $_" -ForegroundColor Red
        return $false
    }

    Start-Sleep -Seconds 10
    Remove-Item -Path $installerPath -Force -ErrorAction SilentlyContinue

    $chromePath = Test-ChromeInstalled
    if ($chromePath) {
        Write-Host "Chrome installed successfully at: $chromePath" -ForegroundColor Green
        return $true
    } else {
        Write-Host "Chrome installation may have failed. Please install manually." -ForegroundColor Red
        return $false
    }
}

# Function to download and install ChromeDriver
function Install-ChromeDriver {
    param(
        [string]$ChromeVersion,
        [string]$DestDir
    )

    $majorVersion = $ChromeVersion.Split('.')[0]
    Write-Host "Chrome major version: $majorVersion" -ForegroundColor Cyan

    $apiUrl = "https://googlechromelabs.github.io/chrome-for-testing/latest-versions-per-milestone-with-downloads.json"

    Write-Host "Fetching matching ChromeDriver version..." -ForegroundColor Yellow
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        $response = Invoke-RestMethod -Uri $apiUrl -Method Get
        $milestone = $response.milestones.$majorVersion

        if (-not $milestone) {
            Write-Host "No matching ChromeDriver found for Chrome version $majorVersion" -ForegroundColor Red
            Write-Host "Trying latest stable version..." -ForegroundColor Yellow

            $latestUrl = "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"
            $latestResponse = Invoke-RestMethod -Uri $latestUrl -Method Get
            $milestone = @{
                version = $latestResponse.channels.Stable.version
                downloads = $latestResponse.channels.Stable.downloads
            }
        }

        $chromedriverVersion = $milestone.version
        Write-Host "Found ChromeDriver version: $chromedriverVersion" -ForegroundColor Green

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
            return $false
        }

        Write-Host "Download URL: $downloadUrl" -ForegroundColor Cyan

        # Create temp directory
        $tempDir = Join-Path $env:TEMP "chromedriver_temp"
        if (Test-Path $tempDir) {
            Remove-Item -Recurse -Force $tempDir
        }
        New-Item -ItemType Directory -Path $tempDir | Out-Null

        $zipPath = Join-Path $tempDir "chromedriver.zip"

        # Download ChromeDriver
        Write-Host "Downloading ChromeDriver..." -ForegroundColor Yellow
        Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath -UseBasicParsing

        # Extract ChromeDriver
        Write-Host "Extracting ChromeDriver..." -ForegroundColor Yellow
        Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force

        # Find the chromedriver.exe
        $chromedriverExe = Get-ChildItem -Path $tempDir -Recurse -Filter "chromedriver.exe" | Select-Object -First 1

        if (-not $chromedriverExe) {
            Write-Host "Could not find chromedriver.exe in extracted files" -ForegroundColor Red
            Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
            return $false
        }

        # Destination path using passed directory
        $destPath = Join-Path $DestDir "chromedriver.exe"
        Write-Host "Destination: $destPath" -ForegroundColor Gray

        # Stop any running ChromeDriver processes
        Write-Host "Stopping any running ChromeDriver processes..." -ForegroundColor Yellow
        Get-Process -Name "chromedriver" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2

        # Copy to destination
        Copy-Item -Path $chromedriverExe.FullName -Destination $destPath -Force

        # Clean up
        Remove-Item -Recurse -Force $tempDir

        Write-Host "ChromeDriver installed successfully!" -ForegroundColor Green
        Write-Host "Location: $destPath" -ForegroundColor Cyan
        Write-Host "Version: $chromedriverVersion" -ForegroundColor Cyan

        return $true

    } catch {
        Write-Host "Error downloading ChromeDriver: $_" -ForegroundColor Red
        return $false
    }
}

# Function to update .env file
function Update-EnvFile {
    param(
        [string]$ChromePath,
        [bool]$Headless,
        [string]$DestDir
    )

    $envPath = Join-Path $DestDir ".env"

    $envContent = @"
# Rust Scraper Environment Configuration (runs outside Docker)
RUST_LOG=debug
MONGODB_URI=mongodb://REMOTE_HOST_REMOVED:27017/real_estayer
ENVIRONMENT=development

# Chrome/Selenium Configuration for Windows
HEADLESS_BROWSER=$($Headless.ToString().ToLower())
CHROME_DRIVER_PATH=chromedriver.exe
CHROME_BINARY_PATH=$ChromePath
"@

    Set-Content -Path $envPath -Value $envContent
    Write-Host "Updated .env file with Chrome configuration" -ForegroundColor Green
}

# Main execution
Write-Host "Step 1: Checking Chrome installation..." -ForegroundColor Cyan
$chromePath = Test-ChromeInstalled

if ($chromePath -and -not $Force) {
    $chromeVersion = Get-ChromeVersion -ChromePath $chromePath
    Write-Host "Chrome is already installed!" -ForegroundColor Green
    Write-Host "  Path: $chromePath" -ForegroundColor White
    Write-Host "  Version: $chromeVersion" -ForegroundColor White
} else {
    if ($Force) {
        Write-Host "Force flag set - reinstalling Chrome..." -ForegroundColor Yellow
    }

    $installed = Install-Chrome
    if (-not $installed) {
        Write-Host "Failed to install Chrome. Exiting." -ForegroundColor Red
        exit 1
    }

    $chromePath = Test-ChromeInstalled
    $chromeVersion = Get-ChromeVersion -ChromePath $chromePath
}

Write-Host ""
Write-Host "Step 2: Installing matching ChromeDriver..." -ForegroundColor Cyan

if (-not $chromeVersion) {
    Write-Host "Could not detect Chrome version. Exiting." -ForegroundColor Red
    exit 1
}

$driverInstalled = Install-ChromeDriver -ChromeVersion $chromeVersion -DestDir $ScriptDir
if (-not $driverInstalled) {
    Write-Host "Failed to install ChromeDriver. Exiting." -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "Step 3: Updating configuration..." -ForegroundColor Cyan
Update-EnvFile -ChromePath $chromePath -Headless:$Headless -DestDir $ScriptDir

Write-Host ""
Write-Host "=========================================" -ForegroundColor Green
Write-Host "  Setup Complete!                       " -ForegroundColor Green
Write-Host "=========================================" -ForegroundColor Green
Write-Host ""
Write-Host "Chrome and ChromeDriver are now configured." -ForegroundColor White
Write-Host ""
Write-Host "To start the Rust scraper:" -ForegroundColor Cyan
Write-Host "  cargo run --release" -ForegroundColor Yellow
Write-Host ""
Write-Host "The scraper will be available at:" -ForegroundColor Cyan
Write-Host "  http://REMOTE_HOST_REMOVED:3001" -ForegroundColor Yellow
Write-Host ""
Write-Host "To verify the setup, run:" -ForegroundColor Cyan
Write-Host "  curl http://localhost:3001/health" -ForegroundColor Yellow
Write-Host ""

# Optionally start the scraper
$startNow = Read-Host "Would you like to build and start the scraper now? (y/N)"
if ($startNow -eq "y" -or $startNow -eq "Y") {
    Write-Host "Building and starting Rust scraper..." -ForegroundColor Cyan
    Set-Location $ScriptDir
    cargo run --release
}
