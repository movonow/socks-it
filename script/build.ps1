$outputDir = Join-Path $PSScriptRoot "../bin"
mkdir $outputDir -ErrorAction SilentlyContinue
$outputDir = Resolve-Path $outputDir

$programs = @(
    "client",
    "server"
)

$targets = @(
    @{ OS = "linux"; Arch = "amd64";  Output = "linux-amd64" },
    @{ OS = "linux"; Arch = "arm64";  Output = "linux-arm64" },
    @{ OS = "windows"; Arch = "amd64"; Output = "windows-amd64.exe" },
    @{ OS = "windows"; Arch = "arm64"; Output = "windows-arm64.exe" }
)

foreach ($program in $programs) {
    foreach ($target in $targets) {
        $env:CGO_ENABLED = 0
        $env:GOOS = $target.OS
        $env:GOARCH = $target.Arch

        Write-Host "Building for $($target.OS)/$($target.Arch)..."

        # Run the Go build command with output to the specified directory
        $filename =  $program+"-"+$target.Output
        $outputPath = Join-Path $outputDir $filename
        go build --trimpath -ldflags="-s -w" -o $outputPath "socks.it/proxy/bin/$program"

        if ($LASTEXITCODE -eq 0) {
            Write-Host "Successfully built: $outputPath"
        } else {
            Write-Host "Failed to build: $($target.OS)/$($target.Arch)" -ForegroundColor Red
        }
    }
}
# Clean up environment variables
Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH

$program = "generateSuite.exe"
$package = "socks.it/nothing/bin"
Write-Host "Building $program for local machine..."
$outputPath = Join-Path $outputDir $program
go build -o $outputPath $package
if ($LASTEXITCODE -eq 0) {
    Write-Host "Successfully built: $outputPath"
} else {
    Write-Host "Failed to build: $package" -ForegroundColor Red
}

Write-Host "All builds completed!"