#/bin/sh

function test {
  echo "+ $@"
  "$@"
  local status=$?
  if [ $status -ne 0 ]; then
    exit $status
  fi
  return $status
}

GIT_VERSION=`cd ${GOPATH}/src/github.com/elgatito/elementum; git describe --tags`
cd $GOPATH
# set -x
# test go build -tags="disable_libutp" -ldflags="-w -X github.com/bcrusher29/solaris/util.Version=\"${GIT_VERSION}\"" -o /var/tmp/elementum github.com/elgatito/elementum
test go build -ldflags="-w -X github.com/bcrusher29/solaris/util.Version=${GIT_VERSION}" -o /var/tmp/elementum github.com/elgatito/elementum
test chmod +x /var/tmp/elementum
test cp -rf /var/tmp/elementum $HOME/.kodi/addons/plugin.video.elementum/resources/bin/linux_x64/
test cp -rf /var/tmp/elementum $HOME/.kodi/userdata/addon_data/plugin.video.elementum/bin/linux_x64/
