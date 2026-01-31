# Ansible Standards

A comprehensive guide to creating production-quality Ansible collections, roles,
playbooks, and testing with Molecule. This document serves as the knowledge base
for the ansible-review pattern.

---

## Table of Contents

- [The Zen of Ansible](#the-zen-of-ansible)
- [Collections](#collections)
- [Roles](#roles)
- [Playbooks](#playbooks)
- [Naming Conventions](#naming-conventions)
- [Task Quality](#task-quality)
- [Molecule Testing](#molecule-testing)
- [Linting](#linting)
- [Security Best Practices](#security-best-practices)
- [Anti-Patterns](#anti-patterns)
- [Quality Checklist](#quality-checklist)

---

## The Zen of Ansible

> *By Tim Appnel*

Before diving into specifics, understand the guiding philosophy:

- **Ansible is not Python** - YAML sucks for coding
- **Playbooks are not for programming** - Ansible users are (most probably) not programmers
- **Clear is better than cluttered**
- **Concise is better than verbose**
- **Simple is better than complex**
- **Readability counts**
- **Helping users get things done matters most**
- **User experience beats ideological purity**

---

## Collections

Collections are the standard distribution format for Ansible content, bundling
roles, modules, plugins, and playbooks into a single distributable unit.

### Collection Structure

```
my_namespace/my_collection/
├── docs/                          # Documentation (markdown only)
├── galaxy.yml                     # Required: collection metadata
├── meta/
│   └── runtime.yml                # Ansible version requirements, routing
├── plugins/
│   ├── modules/                   # Custom modules
│   ├── module_utils/              # Shared Python code for modules
│   ├── plugin_utils/              # Shared Python code for plugins
│   ├── inventory/                 # Inventory plugins
│   ├── lookup/                    # Lookup plugins
│   ├── filter/                    # Filter plugins
│   ├── callback/                  # Callback plugins
│   └── connection/                # Connection plugins
├── roles/
│   ├── role_one/
│   └── role_two/
├── playbooks/
│   ├── files/
│   ├── vars/
│   ├── templates/
│   └── tasks/
├── tests/                         # Integration tests
├── README.md                      # Required: markdown readme
├── CHANGELOG.md
└── LICENSE
```

### galaxy.yml Requirements

The `galaxy.yml` file is required at the collection root:

```yaml
---
# Required fields
namespace: my_namespace              # lowercase, alphanumeric, underscores
name: my_collection                  # lowercase, alphanumeric, underscores
version: "1.0.0"                     # semantic versioning
readme: README.md                    # path to markdown readme
authors:
  - "Your Name <email@example.com>"

# Recommended fields
description: "Brief description of the collection"
license:
  - MIT                              # SPDX license identifier
license_file: LICENSE                # Alternative to license list

# Discovery and linking
tags:
  - networking                       # lowercase, max 20 tags, 64 chars each
  - automation
  - infrastructure
repository: "https://github.com/org/collection"
documentation: "https://docs.example.com"
homepage: "https://example.com"
issues: "https://github.com/org/collection/issues"

# Dependencies on other collections
dependencies:
  ansible.netcommon: ">=2.0.0"
  community.general: ">=4.0.0,<6.0.0"

# Build configuration
build_ignore:
  - "*.tar.gz"
  - ".git"
  - ".github"
  - "tests/output"
```

**Valid tags include:** `application`, `cloud`, `database`, `infrastructure`,
`linux`, `monitoring`, `networking`, `security`, `storage`, `tools`, `windows`

### Collection Best Practices

1. **Use Fully Qualified Collection Names (FQCN)**

   ```yaml
   # Good
   - name: Install package
     ansible.builtin.package:
       name: nginx

   # Bad - avoid short names
   - name: Install package
     package:
       name: nginx
   ```

2. **Centralize plugins** - Put plugins in `plugins/` directory, not in
   individual roles

3. **Namespace prevents conflicts** - Collections give your roles a namespace,
   eliminating naming collisions

4. **Version semantically** - Use `1.0.0+` for production-ready collections

5. **Include runtime.yml** - Specify minimum Ansible version:

   ```yaml
   # meta/runtime.yml
   ---
   requires_ansible: ">=2.14.0"
   ```

---

## Roles

Roles encapsulate reusable automation logic with a standardized structure.

### Role Structure

```
roles/my_role/
├── defaults/
│   └── main.yml           # Default variables (lowest precedence)
├── vars/
│   └── main.yml           # Role variables (higher precedence)
├── tasks/
│   └── main.yml           # Task entry point
├── handlers/
│   └── main.yml           # Handler definitions
├── templates/             # Jinja2 templates
├── files/                 # Static files
├── meta/
│   ├── main.yml           # Role metadata, dependencies
│   └── argument_specs.yml # Input validation (Ansible 2.11+)
├── molecule/              # Molecule test scenarios
│   └── default/
│       ├── molecule.yml
│       ├── converge.yml
│       └── verify.yml
└── README.md
```

### Role Design Principles

1. **Single responsibility** - Each role should have one clear purpose

   ```yaml
   # Good: focused roles
   roles:
     - nginx_install
     - nginx_configure
     - ssl_certificates

   # Bad: monolithic roles
   roles:
     - webserver_everything
   ```

2. **Limit dependencies** - Keep roles loosely coupled

   ```yaml
   # meta/main.yml - minimize dependencies
   ---
   dependencies: []  # Prefer explicit role inclusion in playbooks
   ```

3. **Fail fast with argument specs** - Validate inputs at role start

   ```yaml
   # meta/argument_specs.yml
   ---
   argument_specs:
     main:
       short_description: Configure nginx
       options:
         nginx_port:
           type: int
           required: true
           description: Port nginx listens on
         nginx_user:
           type: str
           default: www-data
           description: User nginx runs as
   ```

4. **Design for check mode**

   ```yaml
   - name: Get service status
     ansible.builtin.command: systemctl status nginx
     register: nginx_status
     changed_when: false  # Read-only command
     check_mode: false    # Run even in check mode
   ```

### Variable Management

**Defaults vs Vars:**

| Location | Precedence | Use Case |
|----------|------------|----------|
| `defaults/main.yml` | Lowest | User-configurable values |
| `vars/main.yml` | Higher | Internal constants, magic values |

**Naming conventions:**

```yaml
# defaults/main.yml
---
# Prefix with role name to avoid collisions
nginx_port: 80
nginx_worker_processes: auto
nginx_worker_connections: 1024

# Internal variables use double underscore prefix
__nginx_default_config_path: /etc/nginx/nginx.conf
```

**Platform-specific variables:**

```yaml
# tasks/main.yml
---
- name: Load OS-specific variables
  ansible.builtin.include_vars: "{{ item }}"
  with_first_found:
    - files:
        - "{{ ansible_facts['distribution'] }}-{{ ansible_facts['distribution_major_version'] }}.yml"
        - "{{ ansible_facts['distribution'] }}.yml"
        - "{{ ansible_facts['os_family'] }}.yml"
        - default.yml
      paths:
        - vars
```

### Multi-Platform Support

```yaml
# tasks/main.yml
---
- name: Include OS-specific tasks
  ansible.builtin.include_tasks: "{{ item }}"
  with_first_found:
    - files:
        - "install-{{ ansible_facts['distribution'] | lower }}.yml"
        - "install-{{ ansible_facts['os_family'] | lower }}.yml"
        - install-default.yml
      paths:
        - tasks
```

---

## Playbooks

Playbooks orchestrate roles and tasks against inventory.

### Playbook Structure

```yaml
---
# playbooks/site.yml
# Description: Main site playbook for web infrastructure
# Usage: ansible-playbook playbooks/site.yml -i inventory/

- name: Configure web servers
  hosts: "{{ target | default('webservers') }}"
  become: true
  gather_facts: true
  # Gather only needed facts for performance
  gather_subset:
    - network
    - hardware

  vars_files:
    - vars/common.yml

  pre_tasks:
    - name: Update apt cache
      ansible.builtin.apt:
        update_cache: true
        cache_valid_time: 3600
      when: ansible_facts['os_family'] == 'Debian'

  roles:
    - role: common
      tags: [common]
    - role: nginx
      tags: [nginx, web]
    - role: ssl
      tags: [ssl, security]

  post_tasks:
    - name: Verify services
      ansible.builtin.service_facts:
```

**Key principles:**

1. **Keep playbooks minimal** - Delegate logic to roles
2. **Use either `tasks` OR `roles`** - Not both in the same play
3. **Default hosts safely** - Use `"{{ target | default('all') }}"`
4. **Start with YAML document marker** - Always begin with `---`

### Tags

```yaml
# Apply tags at role level
roles:
  - role: nginx
    tags:
      - nginx
      - web
      - never  # Skip unless explicitly called
```

**Tag best practices:**

- Create meaningful, purpose-based tags
- Document all tags and their purposes
- Avoid destructive sequences requiring specific ordering

### Idempotency

Roles must produce the same result on repeated runs:

```yaml
# Good: idempotent
- name: Ensure nginx is installed
  ansible.builtin.package:
    name: nginx
    state: present

# Bad: non-idempotent
- name: Install nginx
  ansible.builtin.command: apt-get install -y nginx

# If using command/shell, add idempotency guards
- name: Initialize database
  ansible.builtin.command: /opt/app/init_db.sh
  args:
    creates: /opt/app/.db_initialized
```

**Report changes accurately:**

```yaml
- name: Check application version
  ansible.builtin.command: /opt/app/version.sh
  register: app_version
  changed_when: false  # Read-only, never changed

- name: Update configuration
  ansible.builtin.template:
    src: config.j2
    dest: /etc/app/config.yml
  register: config_result
  changed_when: config_result.changed
```

---

## Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Collections | `namespace.collection_name` | `acme.networking` |
| Roles | lowercase, underscores | `nginx_reverse_proxy` |
| Variables | lowercase, underscores, role prefix | `nginx_worker_count` |
| Internal vars | double underscore prefix | `__nginx_internal_flag` |
| Tasks | Start with verb, descriptive | `Ensure nginx is running` |
| Handlers | Describe action | `Restart nginx` |
| Tags | lowercase, hyphenated | `web-server`, `database` |
| Files | lowercase, descriptive | `nginx.conf.j2` |

---

## Task Quality

### FQCN Usage

Always use Fully Qualified Collection Names:

```yaml
# Good
- name: Install packages
  ansible.builtin.apt:
    name: nginx
    state: present

- name: Copy configuration
  ansible.builtin.template:
    src: nginx.conf.j2
    dest: /etc/nginx/nginx.conf

# Bad - short module names
- name: Install packages
  apt:
    name: nginx
```

### Task Naming

```yaml
# Good - descriptive, starts with verb
- name: Ensure nginx configuration directory exists
  ansible.builtin.file:
    path: /etc/nginx/conf.d
    state: directory
    mode: "0755"

# Bad - vague or missing name
- file:
    path: /etc/nginx/conf.d
    state: directory
```

### Quoting

```yaml
# Always quote:
# - Strings starting with {{ or {%
# - Booleans in YAML
# - Octal values (file modes)

- name: Set file permissions
  ansible.builtin.file:
    path: /etc/app/config.yml
    mode: "0644"  # Quoted to prevent octal interpretation

- name: Enable service
  ansible.builtin.service:
    name: nginx
    enabled: true  # Use true/false, not yes/no

- name: Template configuration
  ansible.builtin.template:
    src: "{{ config_template }}"  # Quoted Jinja2
    dest: /etc/app/config.yml
```

### Conditional Logic

```yaml
# Good - simple conditionals
- name: Install packages on Debian
  ansible.builtin.apt:
    name: nginx
  when: ansible_facts['os_family'] == 'Debian'

# Good - combine related conditions
- name: Configure firewall
  ansible.builtin.firewalld:
    port: "{{ nginx_port }}/tcp"
    permanent: true
    state: enabled
  when:
    - ansible_facts['os_family'] == 'RedHat'
    - nginx_enable_firewall | default(true)

# Bad - complex logic in when clauses
- name: Do something complicated
  ansible.builtin.debug:
    msg: "Complex"
  when: >-
    (var1 == 'a' and var2 == 'b') or
    (var3 == 'c' and var4 in ['d', 'e', 'f']) or
    (var5 | length > 3)
```

---

## Molecule Testing

Molecule provides a testing framework for developing and validating Ansible
roles.

### Scenario Structure

```
roles/my_role/
└── molecule/
    ├── default/                    # Default scenario
    │   ├── molecule.yml            # Scenario configuration
    │   ├── converge.yml            # Playbook to test role
    │   ├── verify.yml              # Verification playbook
    │   ├── prepare.yml             # Pre-test setup (optional)
    │   ├── cleanup.yml             # Post-test cleanup (optional)
    │   └── side_effect.yml         # Side effect playbook (optional)
    └── security/                   # Additional scenario
        ├── molecule.yml
        ├── converge.yml
        └── verify.yml
```

### molecule.yml Configuration

```yaml
---
# molecule/default/molecule.yml
dependency:
  name: galaxy
  options:
    requirements-file: requirements.yml

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
    ssh_connection:
      pipelining: true
  inventory:
    group_vars:
      all:
        ansible_user: root
  playbooks:
    converge: converge.yml
    verify: verify.yml

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

### Test Playbooks

**converge.yml** - Apply the role:

```yaml
---
# molecule/default/converge.yml
- name: Converge
  hosts: all
  become: true

  vars:
    nginx_port: 8080
    nginx_worker_processes: 2

  roles:
    - role: "{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') | basename }}"
```

**verify.yml** - Validate the result:

```yaml
---
# molecule/default/verify.yml
- name: Verify
  hosts: all
  become: true
  gather_facts: true

  tasks:
    - name: Check nginx is installed
      ansible.builtin.package:
        name: nginx
        state: present
      check_mode: true
      register: nginx_installed
      failed_when: nginx_installed.changed

    - name: Check nginx is running
      ansible.builtin.service:
        name: nginx
        state: started
        enabled: true
      check_mode: true
      register: nginx_running
      failed_when: nginx_running.changed

    - name: Verify nginx responds
      ansible.builtin.uri:
        url: "http://localhost:8080"
        status_code: 200
      register: nginx_response
      retries: 3
      delay: 5
      until: nginx_response.status == 200
```

### Multi-Platform Testing

```yaml
---
platforms:
  - name: ubuntu-20
    image: geerlingguy/docker-ubuntu2004-ansible:latest
    pre_build_image: true
    groups:
      - debian

  - name: ubuntu-22
    image: geerlingguy/docker-ubuntu2204-ansible:latest
    pre_build_image: true
    groups:
      - debian

  - name: rocky-8
    image: geerlingguy/docker-rockylinux8-ansible:latest
    pre_build_image: true
    groups:
      - redhat

  - name: rocky-9
    image: geerlingguy/docker-rockylinux9-ansible:latest
    pre_build_image: true
    groups:
      - redhat
```

---

## Linting

### ansible-lint Configuration

```yaml
# .ansible-lint
---
profile: production  # min, basic, moderate, safety, shared, production

# Enable optional rules
enable_list:
  - args
  - empty-string-compare
  - no-log-password
  - no-same-owner

# Skip specific rules (use sparingly)
skip_list:
  - role-name[path]  # When role path doesn't match name

# Exclude paths
exclude_paths:
  - .github/
  - .cache/
  - molecule/

# Warn instead of fail
warn_list:
  - experimental

# Offline mode (no network calls)
offline: false
```

### Common Rules

| Rule | Description |
|------|-------------|
| `yaml[line-length]` | Lines should be <=160 chars |
| `name[casing]` | Task names should be capitalized |
| `fqcn[action-core]` | Use FQCN for builtin modules |
| `no-changed-when` | Commands should have `changed_when` |
| `risky-shell-pipe` | Shell pipes can hide failures |
| `package-latest` | Avoid `state: latest` |

### YAML Style

```yaml
# .yamllint
---
extends: default

rules:
  line-length:
    max: 160
    level: warning
  truthy:
    allowed-values:
      - "true"
      - "false"
  comments:
    min-spaces-from-content: 1
  braces:
    min-spaces-inside: 0
    max-spaces-inside: 1
  octal-values:
    forbid-implicit-octal: true
    forbid-explicit-octal: true
```

---

## Security Best Practices

### 1. Never Store Secrets in Plain Text

```bash
# Encrypt sensitive files
ansible-vault encrypt vars/secrets.yml

# Use vault in playbooks
ansible-playbook site.yml --ask-vault-pass
```

### 2. Use no_log for Sensitive Tasks

```yaml
- name: Set database password
  ansible.builtin.user:
    name: dbuser
    password: "{{ db_password | password_hash('sha512') }}"
  no_log: true
```

### 3. Avoid Committing Sensitive Files

```gitignore
# .gitignore
*.vault.yml
**/secrets/
.env
```

### 4. Validate External Input

```yaml
- name: Validate input
  ansible.builtin.assert:
    that:
      - nginx_port | int > 0
      - nginx_port | int < 65536
    fail_msg: "Invalid port number"
```

### 5. Manage Templates Safely

```jinja2
# Add managed marker
{{ ansible_managed | comment }}

# Avoid revealing secrets in error messages
{% if db_password is not defined %}
# ERROR: db_password not set
{% endif %}
```

---

## Anti-Patterns

### Collection Anti-Patterns

| Anti-Pattern | Problem | Solution |
|--------------|---------|----------|
| Short module names | Ambiguous, may conflict | Use FQCN: `ansible.builtin.apt` |
| Missing galaxy.yml | Cannot distribute | Add required metadata |
| Plugins in roles | Hard to share, test | Centralize in `plugins/` |
| No runtime.yml | Version mismatches | Specify `requires_ansible` |

### Role Anti-Patterns

| Anti-Pattern | Problem | Solution |
|--------------|---------|----------|
| Monolithic roles | Hard to test, reuse | Single responsibility |
| Hardcoded values | Not reusable | Use defaults/vars |
| No argument specs | Silent failures | Add validation |
| Excessive dependencies | Coupling, conflicts | Minimize dependencies |

### Task Anti-Patterns

| Anti-Pattern | Problem | Solution |
|--------------|---------|----------|
| `command`/`shell` without guards | Not idempotent | Use `creates`/`removes` |
| Missing `changed_when` | False positives | Set explicit conditions |
| `state: latest` | Non-deterministic | Pin versions |
| Unnamed tasks | Hard to debug | Descriptive names |
| Complex Jinja2 in playbooks | Hard to read | Use filters or variables |

### Testing Anti-Patterns

| Anti-Pattern | Problem | Solution |
|--------------|---------|----------|
| No Molecule tests | Untested code | Add default scenario |
| Missing verify.yml | No validation | Add verification tasks |
| Single platform tests | Hidden bugs | Test multiple platforms |
| No idempotence check | Not idempotent | Include in test sequence |

---

## Quality Checklist

### Collection Checklist

- [ ] `galaxy.yml` has all required fields
- [ ] Namespace and name follow conventions
- [ ] Version follows semantic versioning
- [ ] `runtime.yml` specifies minimum Ansible version
- [ ] All modules use FQCN
- [ ] README.md documents collection usage
- [ ] CHANGELOG.md tracks changes

### Role Checklist

- [ ] Single, clear responsibility
- [ ] `defaults/main.yml` for user-configurable values
- [ ] `vars/main.yml` for internal constants only
- [ ] Variables prefixed with role name
- [ ] `argument_specs.yml` validates inputs
- [ ] `meta/main.yml` has correct metadata
- [ ] Platform-specific handling if needed
- [ ] README.md documents role usage

### Playbook Checklist

- [ ] Starts with `---` document marker
- [ ] Play has descriptive `name`
- [ ] Hosts default safely: `"{{ target | default('all') }}"`
- [ ] Uses roles OR tasks, not both
- [ ] Tags are meaningful and documented
- [ ] `become` only when necessary

### Task Checklist

- [ ] All modules use FQCN
- [ ] Every task has a descriptive name
- [ ] Names start with verbs
- [ ] `changed_when` set for commands
- [ ] Idempotency guards for shell/command
- [ ] File modes quoted: `mode: "0644"`
- [ ] Jinja2 expressions quoted

### Molecule Checklist

- [ ] Default scenario exists
- [ ] `converge.yml` applies the role
- [ ] `verify.yml` validates results
- [ ] Multi-platform testing
- [ ] Idempotence in test sequence
- [ ] CI/CD integration configured

### Security Checklist

- [ ] No plaintext secrets in code
- [ ] `no_log: true` for sensitive tasks
- [ ] Input validation with `assert`
- [ ] Vault used for sensitive data
- [ ] Templates include `ansible_managed`

---

## References

- [Ansible Collection Structure - Official Docs](https://docs.ansible.com/ansible/latest/dev_guide/developing_collections_structure.html)
- [Creating Collections - Official Docs](https://docs.ansible.com/ansible/latest/dev_guide/developing_collections_creating.html)
- [Good Practices for Ansible (GPA) - Red Hat CoP](https://redhat-cop.github.io/automation-good-practices/)
- [Ansible Roles - Official Docs](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_reuse_roles.html)
- [Molecule Documentation](https://docs.ansible.com/projects/molecule/)
- [ansible-lint Documentation](https://docs.ansible.com/projects/lint/)

---

*Last updated: 2025-01-15*
