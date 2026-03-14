# ChromeDriver Update Guide

## Problem
The error you're seeing occurs when ChromeDriver version doesn't match your Chrome browser version:
```
This version of ChromeDriver only supports Chrome version 139
Current browser version is 141.0.7390.55
```

## Solution

### Windows Users (Automatic)
Run the provided PowerShell script to automatically download and install the correct ChromeDriver version:

```powershell
cd rust-scraper
.\update-chromedriver.ps1
```

This script will:
1. Detect your Chrome browser version
2. Download the matching ChromeDriver from Google's official CDN
3. Extract and place it in the rust-scraper directory
4. Clean up temporary files

### Manual Installation (All Platforms)

1. **Check your Chrome version**:
   - Open Chrome and go to `chrome://version/`
   - Note the version number (e.g., 141.0.7390.55)

2. **Download matching ChromeDriver**:
   - Visit: https://googlechromelabs.github.io/chrome-for-testing/
   - Find your Chrome major version (e.g., 141)
   - Download the appropriate ChromeDriver for your platform:
     - Windows: `chromedriver-win64.zip`
     - macOS: `chromedriver-mac-x64.zip` or `chromedriver-mac-arm64.zip`
     - Linux: `chromedriver-linux64.zip`

3. **Install ChromeDriver**:

   **Windows**:
   ```powershell
   # Extract the downloaded zip file
   # Copy chromedriver.exe to the rust-scraper directory
   # Or set environment variable:
   $env:CHROME_DRIVER_PATH = "C:\path\to\chromedriver.exe"
   ```

   **macOS/Linux**:
   ```bash
   # Extract the downloaded zip file
   unzip chromedriver-*.zip

   # Make it executable
   chmod +x chromedriver

   # Option 1: Move to rust-scraper directory
   mv chromedriver /path/to/rust-scraper/

   # Option 2: Move to system path
   sudo mv chromedriver /usr/local/bin/

   # Option 3: Set environment variable
   export CHROME_DRIVER_PATH="/path/to/chromedriver"
   ```

4. **Verify installation**:
   ```bash
   # Check ChromeDriver version
   chromedriver --version
   ```

## Environment Variable
You can set the `CHROME_DRIVER_PATH` environment variable to specify a custom ChromeDriver location:

**Windows**:
```powershell
$env:CHROME_DRIVER_PATH = "C:\path\to\chromedriver.exe"
```

**macOS/Linux**:
```bash
export CHROME_DRIVER_PATH="/path/to/chromedriver"
```

Add this to your `.env` file in the rust-scraper directory for persistence.

## Preventing Future Issues

### Option 1: Disable Chrome Auto-Update (Not Recommended)
This can create security vulnerabilities.

### Option 2: Regular Updates
- Check for ChromeDriver updates weekly
- Run the update script after Chrome updates
- Subscribe to Chrome release notifications

### Option 3: Use Chrome for Testing
Download and use Chrome for Testing which has stable versions:
https://googlechromelabs.github.io/chrome-for-testing/

## Troubleshooting

### ChromeDriver won't start
- Make sure no other ChromeDriver processes are running
- Check file permissions (should be executable)
- Verify the path in `CHROME_DRIVER_PATH` is correct

### Version still mismatched
- Completely stop all ChromeDriver processes
- Delete old chromedriver binary
- Re-run the update script or manually download
- Restart your Rust scraper service

### Port 9515 already in use
```bash
# Windows
netstat -ano | findstr :9515
taskkill /PID <pid> /F

# macOS/Linux
lsof -ti:9515 | xargs kill -9
```

## Additional Resources
- Chrome for Testing: https://googlechromelabs.github.io/chrome-for-testing/
- ChromeDriver Documentation: https://chromedriver.chromium.org/
- thirtyfour Rust crate: https://docs.rs/thirtyfour/
