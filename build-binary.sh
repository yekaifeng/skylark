#!/bin/bash -x

if [ $# != 2 ]
then
  echo "usage:   build-binary.sh source_directory version"
  echo "example: ./build-binary.sh oam-docker-ipam 1.0.1"
  echo "this script require these environemt: go, tar, bzip2"
  echo "final product is the binary file in bin directory"
  exit 1
fi

cwd=$(pwd)
source_dir=$1
version=$2
export GOPATH=$cwd
export GOBIN=$GOPATH/bin

rm -rf $GOBIN
rm -rf $GOPATH/src
rm -rf $GOPATH/pkg
mkdir -p $GOBIN
mkdir -p $GOPATH/src
go env

# copy go deps into src dir
echo "copying source ..."
cp -rf $GOPATH/$source_dir/Godeps/_workspace/src/* $GOPATH/src/
cp -rf $GOPATH/$source_dir $GOPATH/src

# create version info
sed -i "s/1.0.0/$version/g" $GOPATH/src/$source_dir/main.go

# go build
echo "building source..."
cd $GOPATH/src
go install -v ./oam-docker-ipam

echo "completed !!"


