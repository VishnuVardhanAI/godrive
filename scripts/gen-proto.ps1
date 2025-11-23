Set-Location $PSScriptRoot\..
docker run --rm -v ${PWD}:/work -w /work bufbuild/buf:latest generate