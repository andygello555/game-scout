ROOT="$(git rev-parse --show-toplevel)"
UNAME="$(uname)"

cd "$ROOT/ScoutWebPipes"
if [ "$UNAME" == "Linux" ]; then
  echo "Running 'dotnet publisher -r linux-x64 --no-self-contained' in $(pwd)"
  dotnet publish -r linux-x64 --no-self-contained
elif [ "$UNAME" == "Darwin" ]; then
  echo "Running 'dotnet publisher -r osx.12-arm64 --no-self-contained' in $(pwd)"
  dotnet publish -r osx.12-arm64 --no-self-contained
fi
cd "$ROOT"
echo "Building game-scout binary in $(pwd)"
go build
