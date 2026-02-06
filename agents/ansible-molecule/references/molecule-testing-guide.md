# Molecule Testing Guide

A comprehensive guide to testing Ansible roles with Molecule. This document serves
as the knowledge base for the ansible-molecule agent.

---

## Table of Contents

- [Overview](#overview)
- [Installation](#installation)
- [Scenario Structure](#scenario-structure)
- [molecule.yml Configuration](#moleculeyml-configuration)
- [Test Playbooks](#test-playbooks)
- [Verification Patterns](#verification-patterns)
- [Multi-Platform Testing](#multi-platform-testing)
- [CI/CD Integration](#cicd-integration)
- [Common Anti-Patterns](#common-anti-patterns)
- [Troubleshooting](#troubleshooting)

---

## Overview

Molecule provides a testing framework for developing and validating Ansible roles.
It supports multiple drivers (Docker, Podman, Vagrant, cloud providers) and enables:

- **Linting** — Validate syntax and style
- **Provisioning** — Create test instances
- **Convergence** — Apply the role
- **Idempotence** — Verify no changes on re-run
- **Verification** — Assert expected state

---

## Installation

```bash
# Core installation
pip install molecule molecule-docker

# With additional drivers
pip install molecule[docker,podman,vagrant]

# Initialize a new role with Molecule
molecule init role my_role

# Add Molecule to existing role
cd roles/my_role
molecule init scenario
```

### Requirements File

```yaml
# requirements.txt or pyproject.toml dependencies
molecule>=6.0.0
molecule-docker>=2.0.0
ansible-lint>=6.0.0
pytest-testinfra>=8.0.0  # Optional: for Python-based verification
```

---

## Scenario Structure

```
roles/my_role/
└── molecule/
    ├── default/                    # Default scenario
    │   ├── molecule.yml            # Scenario configuration
    │   ├── converge.yml            # Playbook to apply role
    │   ├── verify.yml              # Verification playbook
    │   ├── prepare.yml             # Pre-test setup (optional)
    │   ├── cleanup.yml             # Post-test cleanup (optional)
    │   └── side_effect.yml         # Side effect playbook (optional)
    ├── security/                   # Security-focused scenario
    │   ├── molecule.yml
    │   ├── converge.yml
    │   └── verify.yml
    └── upgrade/                    # Upgrade testing scenario
        ├── molecule.yml
        ├── converge.yml
        └── verify.yml
```

### Scenario Purposes

| Scenario | Purpose |
|----------|---------|
| `default` | Standard functionality testing |
| `security` | Security hardening verification |
| `upgrade` | Version upgrade path testing |
| `minimal` | Bare minimum configuration |
| `full` | All features enabled |

---

## molecule.yml Configuration

### Complete Example

```yaml
---
# molecule/default/molecule.yml
dependency:
  name: galaxy
  options:
    requirements-file: requirements.yml
    force: false

driver:
  name: docker

platforms:
  - name: ubuntu-22
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true
    privileged: true
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw
    cgroupns_mode: host
    command: ""
    tmpfs:
      - /run
      - /tmp

  - name: rocky-9
    image: geerlingguy/docker-rockylinux9-ansible:latest
    pre_build_image: true
    privileged: true
    volumes:
      - /sys/fs/cgroup:/sys/fs/cgroup:rw
    cgroupns_mode: host
    command: ""

provisioner:
  name: ansible
  log: true
  config_options:
    defaults:
      callbacks_enabled: profile_tasks
      fact_caching: jsonfile
      fact_caching_connection: /tmp/facts_cache
      gathering: smart
    ssh_connection:
      pipelining: true
  inventory:
    group_vars:
      all:
        ansible_user: root
  playbooks:
    converge: converge.yml
    verify: verify.yml
    prepare: prepare.yml
  env:
    ANSIBLE_FORCE_COLOR: "true"
    ANSIBLE_VERBOSITY: "1"

verifier:
  name: ansible

scenario:
  test_sequence:
    - dependency
    - cleanup
    - destroy
    - syntax
    - create
    - prepare
    - converge
    - idempotence
    - verify
    - cleanup
    - destroy
```

### Driver Options

#### Docker (Recommended)

```yaml
driver:
  name: docker

platforms:
  - name: instance
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true  # Use pre-built image (faster)
    privileged: true       # Required for systemd
    cgroupns_mode: host    # Required for systemd on newer Docker
```

#### Podman

```yaml
driver:
  name: podman

platforms:
  - name: instance
    image: quay.io/ansible/ubuntu2204:latest
    pre_build_image: true
    systemd: always
```

### Dependency Management

```yaml
dependency:
  name: galaxy
  options:
    requirements-file: requirements.yml
    force: false  # Don't re-download if present

# requirements.yml
---
collections:
  - name: community.general
  - name: ansible.posix
roles:
  - name: geerlingguy.docker
```

---

## Test Playbooks

### converge.yml — Apply the Role

```yaml
---
# molecule/default/converge.yml
- name: Converge
  hosts: all
  become: true
  gather_facts: true

  vars:
    # Override defaults for testing
    nginx_port: 8080
    nginx_worker_processes: 2
    nginx_sites:
      - name: test
        server_name: localhost
        root: /var/www/test

  pre_tasks:
    - name: Update apt cache (Debian)
      ansible.builtin.apt:
        update_cache: true
        cache_valid_time: 3600
      when: ansible_os_family == 'Debian'

  roles:
    - role: "{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') | basename }}"
```

### verify.yml — Assert Expected State

```yaml
---
# molecule/default/verify.yml
- name: Verify
  hosts: all
  become: true
  gather_facts: true

  tasks:
    # Package verification
    - name: Check nginx is installed
      ansible.builtin.package:
        name: nginx
        state: present
      check_mode: true
      register: pkg_check
      failed_when: pkg_check.changed

    # Service verification
    - name: Check nginx is running and enabled
      ansible.builtin.service:
        name: nginx
        state: started
        enabled: true
      check_mode: true
      register: svc_check
      failed_when: svc_check.changed

    # Port verification
    - name: Check nginx is listening on port 8080
      ansible.builtin.wait_for:
        port: 8080
        timeout: 10

    # HTTP verification
    - name: Verify nginx responds
      ansible.builtin.uri:
        url: "http://localhost:8080"
        status_code: 200
      register: http_check
      retries: 3
      delay: 5
      until: http_check.status == 200

    # File verification
    - name: Check config file exists
      ansible.builtin.stat:
        path: /etc/nginx/nginx.conf
      register: config_stat
      failed_when: not config_stat.stat.exists

    # Content verification
    - name: Check config contains expected setting
      ansible.builtin.lineinfile:
        path: /etc/nginx/nginx.conf
        line: "worker_processes 2;"
        state: present
      check_mode: true
      register: config_check
      failed_when: config_check.changed
```

### prepare.yml — Pre-Test Setup

```yaml
---
# molecule/default/prepare.yml
- name: Prepare
  hosts: all
  become: true
  gather_facts: true

  tasks:
    - name: Install prerequisites
      ansible.builtin.package:
        name:
          - curl
          - ca-certificates
        state: present

    - name: Create test user
      ansible.builtin.user:
        name: testuser
        state: present
```

### cleanup.yml — Post-Test Cleanup

```yaml
---
# molecule/default/cleanup.yml
- name: Cleanup
  hosts: all
  become: true
  gather_facts: false

  tasks:
    - name: Remove test artifacts
      ansible.builtin.file:
        path: /tmp/molecule_test
        state: absent
```

---

## Verification Patterns

### Pattern 1: Check Mode Verification

Use `check_mode: true` with `failed_when: result.changed` to verify state:

```yaml
- name: Verify package is installed
  ansible.builtin.package:
    name: nginx
    state: present
  check_mode: true
  register: result
  failed_when: result.changed
```

### Pattern 2: Stat + Assert

```yaml
- name: Get file info
  ansible.builtin.stat:
    path: /etc/nginx/nginx.conf
  register: file_stat

- name: Assert file properties
  ansible.builtin.assert:
    that:
      - file_stat.stat.exists
      - file_stat.stat.mode == '0644'
      - file_stat.stat.pw_name == 'root'
    fail_msg: "Config file has incorrect properties"
```

### Pattern 3: Command Output Verification

```yaml
- name: Get nginx version
  ansible.builtin.command:
    cmd: nginx -v
  register: nginx_version
  changed_when: false

- name: Assert nginx version
  ansible.builtin.assert:
    that:
      - "'nginx' in nginx_version.stderr"
    fail_msg: "nginx not properly installed"
```

### Pattern 4: Service Status

```yaml
- name: Get service status
  ansible.builtin.systemd:
    name: nginx
  register: nginx_service

- name: Assert service is active
  ansible.builtin.assert:
    that:
      - nginx_service.status.ActiveState == 'active'
      - nginx_service.status.UnitFileState == 'enabled'
```

### Pattern 5: Network Verification

```yaml
- name: Wait for port
  ansible.builtin.wait_for:
    port: 8080
    host: 127.0.0.1
    timeout: 30

- name: Test HTTP endpoint
  ansible.builtin.uri:
    url: http://localhost:8080/health
    status_code: 200
    return_content: true
  register: health_check

- name: Assert health response
  ansible.builtin.assert:
    that:
      - "'healthy' in health_check.content"
```

---

## Multi-Platform Testing

### Platform Groups

```yaml
platforms:
  - name: ubuntu-20
    image: geerlingguy/docker-ubuntu2004-ansible:latest
    pre_build_image: true
    groups:
      - debian
      - ubuntu

  - name: ubuntu-22
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true
    groups:
      - debian
      - ubuntu

  - name: rocky-8
    image: geerlingguy/docker-rockylinux8-ansible:latest
    pre_build_image: true
    groups:
      - redhat
      - el8

  - name: rocky-9
    image: geerlingguy/docker-rockylinux9-ansible:latest
    pre_build_image: true
    groups:
      - redhat
      - el9

provisioner:
  inventory:
    group_vars:
      debian:
        package_manager: apt
      redhat:
        package_manager: dnf
```

### Platform-Specific Verification

```yaml
# verify.yml
- name: Verify (Debian)
  hosts: debian
  tasks:
    - name: Check apt packages
      ansible.builtin.dpkg_selections:
        name: nginx
        selection: install
      check_mode: true
      register: result
      failed_when: result.changed

- name: Verify (RedHat)
  hosts: redhat
  tasks:
    - name: Check rpm packages
      ansible.builtin.rpm_key:
        state: present
        key: /etc/pki/rpm-gpg/RPM-GPG-KEY-nginx
```

---

## CI/CD Integration

### GitHub Actions

```yaml
# .github/workflows/molecule.yml
---
name: Molecule Test

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  molecule:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        scenario:
          - default
          - security
        distro:
          - ubuntu2204
          - rockylinux9

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Python
        uses: actions/setup-python@v5
        with:
          python-version: '3.11'

      - name: Install dependencies
        run: |
          pip install molecule molecule-docker ansible-lint

      - name: Run Molecule
        run: |
          molecule test -s ${{ matrix.scenario }}
        env:
          MOLECULE_DISTRO: ${{ matrix.distro }}
```

### GitLab CI

```yaml
# .gitlab-ci.yml
---
stages:
  - lint
  - test

variables:
  PIP_CACHE_DIR: "$CI_PROJECT_DIR/.pip-cache"

.molecule:
  image: python:3.11
  services:
    - docker:dind
  before_script:
    - pip install molecule molecule-docker ansible-lint
  cache:
    paths:
      - .pip-cache/

lint:
  extends: .molecule
  stage: lint
  script:
    - ansible-lint

molecule:default:
  extends: .molecule
  stage: test
  script:
    - molecule test -s default
```

---

## Common Anti-Patterns

### Anti-Pattern 1: No Verification

```yaml
# BAD: No verify.yml or empty verification
- name: Verify
  hosts: all
  tasks: []

# GOOD: Meaningful verification
- name: Verify
  hosts: all
  tasks:
    - name: Check service is running
      ansible.builtin.service:
        name: myservice
        state: started
      check_mode: true
      register: result
      failed_when: result.changed
```

### Anti-Pattern 2: Hardcoded Values

```yaml
# BAD: Hardcoded paths and values
- name: Check file
  ansible.builtin.stat:
    path: /home/ubuntu/myapp

# GOOD: Use variables
- name: Check file
  ansible.builtin.stat:
    path: "{{ app_install_dir }}"
```

### Anti-Pattern 3: Missing Idempotence Test

```yaml
# BAD: Skipping idempotence
scenario:
  test_sequence:
    - converge
    - verify

# GOOD: Include idempotence check
scenario:
  test_sequence:
    - converge
    - idempotence
    - verify
```

### Anti-Pattern 4: Privileged Without Need

```yaml
# BAD: Always privileged
platforms:
  - name: instance
    privileged: true  # Only if needed for systemd

# GOOD: Only when necessary, document why
platforms:
  - name: instance
    privileged: true  # Required for systemd services
    # privileged: false  # Use this if no systemd needed
```

### Anti-Pattern 5: Single Platform Testing

```yaml
# BAD: Only one platform
platforms:
  - name: ubuntu
    image: ubuntu:22.04

# GOOD: Multiple platforms
platforms:
  - name: ubuntu-22
    image: geerlingguy/docker-ubuntu2204-ansible:latest
  - name: rocky-9
    image: geerlingguy/docker-rockylinux9-ansible:latest
```

---

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| `systemd not running` | Container not privileged | Add `privileged: true` and `cgroupns_mode: host` |
| `cgroup mount failed` | Missing cgroup volume | Add `/sys/fs/cgroup:/sys/fs/cgroup:rw` volume |
| `Idempotence failed` | Task not idempotent | Use `changed_when`/`creates`/`removes` |
| `Galaxy timeout` | Network issues | Add `force: false` to dependency config |
| `Service won't start` | Systemd issue in container | Use `geerlingguy/*-ansible` images |

### Debug Commands

```bash
# Run with verbose output
molecule --debug test

# Keep instances after failure
molecule test --destroy=never

# Interactive debugging
molecule converge
molecule login
molecule verify
molecule destroy

# Run specific scenario
molecule test -s security

# List instances
molecule list

# Check syntax only
molecule syntax
```

---

## References

- [Molecule Documentation](https://molecule.readthedocs.io/)
- [Ansible Testing Strategies](https://docs.ansible.com/ansible/latest/reference_appendices/test_strategies.html)
- [Jeff Geerling's Ansible Docker Images](https://github.com/geerlingguy/docker-ubuntu2204-ansible)
