%global sname   ai-dba-workbench

Name:           pgedge-%{sname}
Version:        %{ai_dba_workbench_version}
Release:        %{ai_dba_workbench_buildnum}%{?dist}
Summary:        pgEdge AI DBA Workbench - PostgreSQL AI monitoring and management
License:        PostgreSQL License
URL:            https://github.com/pgEdge/ai-dba-workbench
Source0:        ai-dba-server-linux-%{arch}.tar.gz
Source1:        ai-dba-collector-linux-%{arch}.tar.gz
Source2:        ai-dba-alerter-linux-%{arch}.tar.gz
Source3:        ai-dba-client.tar.gz
#Source4:        ai-workbench-docs.tar.gz
Source5:        pgedge-ai-dba-server.service
Source6:        pgedge-ai-dba-collector.service
Source7:        pgedge-ai-dba-alerter.service
Source8:        ai-dba-server.yaml
Source9:        ai-dba-collector.yaml
Source10:       ai-dba-alerter.yaml
Source11:       pgedge-ai-dba-client.nginx
Source12:       LICENSE.md
Source13:       README.md
BuildArch:      %{_arch}

%description
pgEdge AI DBA Workbench provides AI-powered monitoring, alerting, and
management for PostgreSQL databases.

# ============================================================================
# Server Package
# ============================================================================
%package -n pgedge-ai-dba-server
Summary:        pgEdge AI DBA Server
Requires:       pgedge-ai-kb openssl

%description -n pgedge-ai-dba-server
Core AI DBA server providing REST API and AI-powered analysis for
PostgreSQL databases.

# ============================================================================
# Collector Package
# ============================================================================
%package -n pgedge-ai-dba-collector
Summary:        pgEdge AI DBA Collector
Requires:       pgedge-ai-dba-server = %{version}-%{release} openssl

%description -n pgedge-ai-dba-collector
Metrics and data collector agent for the pgEdge AI DBA Workbench.
Collects PostgreSQL performance data and sends it to the AI DBA server.

# ============================================================================
# Alerter Package
# ============================================================================
%package -n pgedge-ai-dba-alerter
Summary:        pgEdge AI DBA Alerter
Requires:       pgedge-ai-dba-server = %{version}-%{release}

%description -n pgedge-ai-dba-alerter
Alerting agent for the pgEdge AI DBA Workbench. Monitors PostgreSQL
health and triggers AI-powered alerts based on configurable rules.

# ============================================================================
# Client Package (Web UI)
# ============================================================================
%package -n pgedge-ai-dba-client
Summary:        pgEdge AI DBA Web Client
Requires:       pgedge-ai-dba-server = %{version}-%{release}
Requires:       nginx
BuildArch:      noarch

%description -n pgedge-ai-dba-client
React-based web interface for the pgEdge AI DBA Workbench. Provides
dashboards, alerting UI, and AI-powered database analysis views.

# ============================================================================
# Docs Package
# ============================================================================
#%%package -n pgedge-ai-dba-docs
#Summary:        pgEdge AI DBA Workbench Documentation
#BuildArch:      noarch
#
#%%description -n pgedge-ai-dba-docs
#Documentation for the pgEdge AI DBA Workbench.

# ============================================================================
# Build Section
# ============================================================================
%prep
mkdir -p %{_builddir}/server %{_builddir}/collector %{_builddir}/alerter %{_builddir}/client
tar -xzf %{SOURCE0} -C %{_builddir}/server
tar -xzf %{SOURCE1} -C %{_builddir}/collector
tar -xzf %{SOURCE2} -C %{_builddir}/alerter
tar -xzf %{SOURCE3} -C %{_builddir}/client
#tar -xzf %%{SOURCE4} -C %%{_builddir}/docs

%pre -n pgedge-ai-dba-server
getent group pgedge >/dev/null || groupadd -r pgedge
getent passwd pgedge >/dev/null || \
    useradd -r -g pgedge -d /var/lib/pgedge -s /sbin/nologin \
    -c "pgEdge Services" pgedge
exit 0

%build
syft dir:%{_builddir}/server -o cyclonedx-json > %{_builddir}/pgedge-ai-dba-server-sbom.json || exit 1
KEY_ID=$(gpg --list-secret-keys --with-colons | awk -F: '/^sec/{print $5}' | head -n 1); export KEY_ID
gpg --armor --detach-sign --output %{_builddir}/pgedge-ai-dba-server-sbom.json.asc %{_builddir}/pgedge-ai-dba-server-sbom.json || exit 1

syft dir:%{_builddir}/collector -o cyclonedx-json > %{_builddir}/pgedge-ai-dba-collector-sbom.json || exit 1
KEY_ID=$(gpg --list-secret-keys --with-colons | awk -F: '/^sec/{print $5}' | head -n 1); export KEY_ID
gpg --armor --detach-sign --output %{_builddir}/pgedge-ai-dba-collector-sbom.json.asc %{_builddir}/pgedge-ai-dba-collector-sbom.json || exit 1

syft dir:%{_builddir}/alerter -o cyclonedx-json > %{_builddir}/pgedge-ai-dba-alerter-sbom.json || exit 1
KEY_ID=$(gpg --list-secret-keys --with-colons | awk -F: '/^sec/{print $5}' | head -n 1); export KEY_ID
gpg --armor --detach-sign --output %{_builddir}/pgedge-ai-dba-alerter-sbom.json.asc %{_builddir}/pgedge-ai-dba-alerter-sbom.json || exit 1

syft dir:%{_builddir}/client -o cyclonedx-json > %{_builddir}/pgedge-ai-dba-client-sbom.json || exit 1
KEY_ID=$(gpg --list-secret-keys --with-colons | awk -F: '/^sec/{print $5}' | head -n 1); export KEY_ID
gpg --armor --detach-sign --output %{_builddir}/pgedge-ai-dba-client-sbom.json.asc %{_builddir}/pgedge-ai-dba-client-sbom.json || exit 1

%install
# Directories
install -d %{buildroot}%{_bindir}
install -d %{buildroot}%{_sysconfdir}/pgedge
install -d %{buildroot}%{_unitdir}
install -d %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-server
install -d %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-collector
install -d %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-alerter
install -d %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-client

# Config files
install -m 640 %{SOURCE8} %{buildroot}%{_sysconfdir}/pgedge/ai-dba-server.yaml
install -m 640 %{SOURCE9} %{buildroot}%{_sysconfdir}/pgedge/ai-dba-collector.yaml
install -m 640 %{SOURCE10} %{buildroot}%{_sysconfdir}/pgedge/ai-dba-alerter.yaml
install -d %{buildroot}%{_datadir}
install -d %{buildroot}/var/lib/pgedge/ai-dba-server
install -d %{buildroot}/var/lib/pgedge/ai-dba-collector
install -d %{buildroot}/var/lib/pgedge/ai-dba-alerter
install -d %{buildroot}/var/log/pgedge/ai-dba-server
install -d %{buildroot}/var/log/pgedge/ai-dba-collector
install -d %{buildroot}/var/log/pgedge/ai-dba-alerter

# Server
install -m 755 %{_builddir}/server/ai-dba-server %{buildroot}%{_bindir}/
install -m 644 %{SOURCE5} %{buildroot}%{_unitdir}/pgedge-ai-dba-server.service
install -m 644 %{SOURCE12} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-server/LICENSE.md
install -m 644 %{SOURCE13} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-server/README.md
mkdir -p %{buildroot}%{_datadir}/pgedge-ai-dba-server
install -p -m 0644 %{_builddir}/pgedge-ai-dba-server-sbom.json %{buildroot}%{_datadir}/pgedge-ai-dba-server/pgedge-ai-dba-server-sbom.json
install -p -m 0644 %{_builddir}/pgedge-ai-dba-server-sbom.json.asc %{buildroot}%{_datadir}/pgedge-ai-dba-server/pgedge-ai-dba-server-sbom.json.asc

# Collector
install -m 755 %{_builddir}/collector/ai-dba-collector %{buildroot}%{_bindir}/
install -m 644 %{SOURCE6} %{buildroot}%{_unitdir}/pgedge-ai-dba-collector.service
install -m 644 %{SOURCE12} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-collector/LICENSE.md
install -m 644 %{SOURCE13} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-collector/README.md
mkdir -p %{buildroot}%{_datadir}/pgedge-ai-dba-collector
install -p -m 0644 %{_builddir}/pgedge-ai-dba-collector-sbom.json %{buildroot}%{_datadir}/pgedge-ai-dba-collector/pgedge-ai-dba-collector-sbom.json
install -p -m 0644 %{_builddir}/pgedge-ai-dba-collector-sbom.json.asc %{buildroot}%{_datadir}/pgedge-ai-dba-collector/pgedge-ai-dba-collector-sbom.json.asc

# Alerter
install -m 755 %{_builddir}/alerter/ai-dba-alerter %{buildroot}%{_bindir}/
install -m 644 %{SOURCE7} %{buildroot}%{_unitdir}/pgedge-ai-dba-alerter.service
install -m 644 %{SOURCE12} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-alerter/LICENSE.md
install -m 644 %{SOURCE13} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-alerter/README.md
mkdir -p %{buildroot}%{_datadir}/pgedge-ai-dba-alerter
install -p -m 0644 %{_builddir}/pgedge-ai-dba-alerter-sbom.json %{buildroot}%{_datadir}/pgedge-ai-dba-alerter/pgedge-ai-dba-alerter-sbom.json
install -p -m 0644 %{_builddir}/pgedge-ai-dba-alerter-sbom.json.asc %{buildroot}%{_datadir}/pgedge-ai-dba-alerter/pgedge-ai-dba-alerter-sbom.json.asc

# Client (web UI)
install -d %{buildroot}%{_datadir}/pgedge/ai-dba-client
cp -r %{_builddir}/client/. %{buildroot}%{_datadir}/pgedge/ai-dba-client/
install -m 644 %{SOURCE12} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-client/LICENSE.md
install -m 644 %{SOURCE13} %{buildroot}%{_defaultdocdir}/pgedge-ai-dba-client/README.md
install -d %{buildroot}/etc/nginx/conf.d
install -m 644 %{SOURCE11} %{buildroot}/etc/nginx/conf.d/pgedge-ai-dba-client.conf
mkdir -p %{buildroot}%{_datadir}/pgedge-ai-dba-client
install -p -m 0644 %{_builddir}/pgedge-ai-dba-client-sbom.json %{buildroot}%{_datadir}/pgedge-ai-dba-client/pgedge-ai-dba-client-sbom.json
install -p -m 0644 %{_builddir}/pgedge-ai-dba-client-sbom.json.asc %{buildroot}%{_datadir}/pgedge-ai-dba-client/pgedge-ai-dba-client-sbom.json.asc

# Docs
#install -d %%{buildroot}%%{_datadir}/pgedge/ai-dba-docs
#cp -r %%{_builddir}/docs/. %%{buildroot}%%{_datadir}/pgedge/ai-dba-docs/

%files -n pgedge-ai-dba-server
%license %{_defaultdocdir}/pgedge-ai-dba-server/LICENSE.md
%doc %{_defaultdocdir}/pgedge-ai-dba-server/README.md
%{_bindir}/ai-dba-server
%{_unitdir}/pgedge-ai-dba-server.service
%{_datadir}/pgedge-ai-dba-server/pgedge-ai-dba-server-sbom.json
%{_datadir}/pgedge-ai-dba-server/pgedge-ai-dba-server-sbom.json.asc
%dir %{_sysconfdir}/pgedge
%config(noreplace) %attr(0640,root,pgedge) %{_sysconfdir}/pgedge/ai-dba-server.yaml
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge/ai-dba-server
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge/ai-dba-server

%files -n pgedge-ai-dba-collector
%license %{_defaultdocdir}/pgedge-ai-dba-collector/LICENSE.md
%doc %{_defaultdocdir}/pgedge-ai-dba-collector/README.md
%{_bindir}/ai-dba-collector
%{_unitdir}/pgedge-ai-dba-collector.service
%{_datadir}/pgedge-ai-dba-collector/pgedge-ai-dba-collector-sbom.json
%{_datadir}/pgedge-ai-dba-collector/pgedge-ai-dba-collector-sbom.json.asc
%config(noreplace) %attr(0640,root,pgedge) %{_sysconfdir}/pgedge/ai-dba-collector.yaml
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge/ai-dba-collector
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge/ai-dba-collector

%files -n pgedge-ai-dba-alerter
%license %{_defaultdocdir}/pgedge-ai-dba-alerter/LICENSE.md
%doc %{_defaultdocdir}/pgedge-ai-dba-alerter/README.md
%{_bindir}/ai-dba-alerter
%{_unitdir}/pgedge-ai-dba-alerter.service
%{_datadir}/pgedge-ai-dba-alerter/pgedge-ai-dba-alerter-sbom.json
%{_datadir}/pgedge-ai-dba-alerter/pgedge-ai-dba-alerter-sbom.json.asc
%config(noreplace) %attr(0640,root,pgedge) %{_sysconfdir}/pgedge/ai-dba-alerter.yaml
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge
%dir %attr(0755,pgedge,pgedge) /var/lib/pgedge/ai-dba-alerter
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge
%dir %attr(0755,pgedge,pgedge) /var/log/pgedge/ai-dba-alerter

%files -n pgedge-ai-dba-client
%license %{_defaultdocdir}/pgedge-ai-dba-client/LICENSE.md
%doc %{_defaultdocdir}/pgedge-ai-dba-client/README.md
%{_datadir}/pgedge/ai-dba-client/
%config(noreplace) /etc/nginx/conf.d/pgedge-ai-dba-client.conf
%{_datadir}/pgedge-ai-dba-client/pgedge-ai-dba-client-sbom.json
%{_datadir}/pgedge-ai-dba-client/pgedge-ai-dba-client-sbom.json.asc

#%%files -n pgedge-ai-dba-docs
#%%{_datadir}/pgedge/ai-dba-docs/

%post -n pgedge-ai-dba-server
touch /var/log/pgedge/ai-dba-server/ai-dba-server.log
chown pgedge:pgedge /var/log/pgedge/ai-dba-server/ai-dba-server.log
%systemd_post pgedge-ai-dba-server.service

%preun -n pgedge-ai-dba-server
%systemd_preun pgedge-ai-dba-server.service

%postun -n pgedge-ai-dba-server
%systemd_postun_with_restart pgedge-ai-dba-server.service

%post -n pgedge-ai-dba-collector
touch /var/log/pgedge/ai-dba-collector/ai-dba-collector.log
chown pgedge:pgedge /var/log/pgedge/ai-dba-collector/ai-dba-collector.log
%systemd_post pgedge-ai-dba-collector.service

%preun -n pgedge-ai-dba-collector
%systemd_preun pgedge-ai-dba-collector.service

%postun -n pgedge-ai-dba-collector
%systemd_postun_with_restart pgedge-ai-dba-collector.service

%post -n pgedge-ai-dba-alerter
touch /var/log/pgedge/ai-dba-alerter/ai-dba-alerter.log
chown pgedge:pgedge /var/log/pgedge/ai-dba-alerter/ai-dba-alerter.log
%systemd_post pgedge-ai-dba-alerter.service

%preun -n pgedge-ai-dba-alerter
%systemd_preun pgedge-ai-dba-alerter.service

%postun -n pgedge-ai-dba-alerter
%systemd_postun_with_restart pgedge-ai-dba-alerter.service

%post -n pgedge-ai-dba-client
if [ $1 -eq 1 ]; then
    nginx -t >/dev/null 2>&1 && systemctl reload nginx >/dev/null 2>&1 || :
fi

%preun -n pgedge-ai-dba-client
if [ $1 -eq 0 ]; then
    systemctl reload nginx >/dev/null 2>&1 || :
fi

%clean
rm -rf %{buildroot}

%changelog
* Mon Jun 08 2026 pgEdge Build Team <support@pgedge.com> - 1.0.0-1
- Update RPM package for pgEdge AI DBA Workbench
* Tue May 26 2026 pgEdge Build Team <support@pgedge.com> - 1.0.0-beta3_1
- Update RPM package for pgEdge AI DBA Workbench
* Thu May 14 2026 pgEdge Build Team <support@pgedge.com> - 1.0.0-beta2_1
- Update RPM package for pgEdge AI DBA Workbench
* Tue Apr 21 2026 pgEdge Build Team <support@pgedge.com> - 1.0.0-beta1_1
- Initial RPM package for pgEdge AI DBA Workbench
