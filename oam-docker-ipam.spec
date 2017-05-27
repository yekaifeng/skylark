Name: oam-docker-ipam
Summary: Provides oam-docker-ipam. Built by kenneth ye.
Version: _version_
Group: Applications
License: Restricted
Release: 1.el7.centos
BuildRoot: %{_builddir}/%{name}-%{version}-%{release}-root

Requires: util-linux
Requires: wondershaper
Requires: iproute

%description
Provides oam-docker-ipam global ip resource management.

%post
systemctl daemon-reload

%postun
systemctl daemon-reload

%build
#tar cvf oam-docker-ipam.tar rpm-build/*
cp /tmp/rpm-build.tar %{_builddir}/archive.tar

%install

mkdir -p $RPM_BUILD_ROOT/
mv archive.tar $RPM_BUILD_ROOT/archive.tar
cd $RPM_BUILD_ROOT/
tar -xf $RPM_BUILD_ROOT/archive.tar
rm $RPM_BUILD_ROOT/archive.tar

%clean
rm -fr $RPM_BUILD_ROOT
echo $RPM_BUILD_ROOT

%files
%defattr(-,root,root)
%config(noreplace) /etc/oam-docker-ipam/oam-docker-ipam.conf
/usr/bin/oam-docker-ipam
/usr/bin/ip-pool-usage
/usr/lib/systemd/system/oam-docker-ipam.service

%changelog
* Tue Dec 1 2016 kenneth.ye
-- rpm repackage

