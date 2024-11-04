$startingDir = (Get-Location).Path

$projectDir = Join-Path $PSScriptRoot ".."
$projectDir = Resolve-Path $projectDir
$projectName = Split-Path -Path $projectDir -Leaf

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tempDir | Out-Null

Copy-Item -Path $projectDir -Destination $tempDir -Recurse
$workDir = Join-Path $tempDir $projectName

Set-Location $workDir

git clean -dfx | Out-Null

$today = Get-Date -Format "yyyy-MM-dd"
$zipFile = Join-Path $projectDir "$projectName.$today.zip"
Compress-Archive -Force -Path $workDir -DestinationPath $zipFile
Set-Location $startingDir
Remove-Item -Path $tempDir -Recurse -Force

Write-Host "Archive created: $zipFile"