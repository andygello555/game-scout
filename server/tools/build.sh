ROOT="$(git rev-parse --show-toplevel)"
UNAME="$(uname)"

cd "$ROOT/ScoutWebPipes"
if [ "$UNAME" == "Linux" ]; then
  dotnet publish -r linux-x64 --no-self-contained
elif [ "$UNAME" == "Darwin" ]; then
  dotnet publish -r osx.12-arm64 --no-self-contained
fi
cd "$ROOT"
go build
