# Совместимость: три профиля через Benchmark-Masque.ps1 (оставшиеся аргументы пробрасываются).
param(
    [switch]$SkipBuild,
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Passthrough
)
$args = @("-NoProfile", "-File", (Join-Path $PSScriptRoot "Benchmark-Masque.ps1"))
if ($SkipBuild) { $args += "-SkipBuild" }
if ($Passthrough -and $Passthrough.Count -gt 0) { $args += $Passthrough }
& powershell @args
exit $LASTEXITCODE
