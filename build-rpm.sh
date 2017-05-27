#!/bin/bash

if [ $# != 2 ]
then
  echo "usage:   build-rpm.sh source_directory version"
  echo "example: ./build-rpm.sh oam-docker-ipam 1.0.1"
  echo "this script require these environemt: go, tar, bzip2"
  echo "final product is the rpm file in home directory"
  exit 1
fi

cwd=$(pwd)
source_dir=$1
version=$2

# build binary first
./build-binary.sh $source_dir $version

# copy oam-docker-ipam binary to rpm-build dir
cp -f ./bin/oam-docker-ipam ./rpm-build/usr/bin

# create tmp archive
cd ./rpm-build
tar cvf /tmp/rpm-build.tar *
cd ..

# build rpm
cp oam-docker-ipam.spec tmp.spec
sed -i "s/_version_/$version/g" tmp.spec
rpmbuild -bb tmp.spec
rm -f tmp.spec

cp -fr ~/rpmbuild/RPMS/x86_64/oam-docker-ipam-$version* ./rpms

echo "rpm build completed!"

