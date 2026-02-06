# Idiomatic Ansible Review Criteria

This document provides comprehensive criteria for reviewing Ansible code. It covers
style conventions, structural patterns, security practices, and common anti-patterns.

**Companion document**: See [ansible-standards.md](./ansible-standards.md) for detailed
coverage of Collections, Roles with argument_specs, and Molecule testing configuration.

---

## Table of Contents

1. [The Zen of Ansible](#the-zen-of-ansible)
2. [YAML Formatting](#yaml-formatting)
3. [Naming Conventions](#naming-conventions)
4. [Task Structure](#task-structure)
5. [Variables](#variables)
6. [Conditionals](#conditionals)
7. [Loops](#loops)
8. [Handlers](#handlers)
9. [Error Handling](#error-handling)
10. [Idempotency](#idempotency)
11. [Role Structure](#role-structure)
12. [Collections and FQCN](#collections-and-fqcn)
13. [Inventory Best Practices](#inventory-best-practices)
14. [Security](#security)
15. [Performance](#performance)
16. [Testing](#testing)
17. [Common Anti-patterns](#common-anti-patterns)

---

## The Zen of Ansible

Core principles that guide idiomatic Ansible:

- **Ansible is not Python** — YAML is challenging for coding
- **Playbooks are not for programming** — most users are not programmers
- **Clear is better than cluttered**
- **Concise is better than verbose**
- **Simple is better than complex**
- **Readability counts**
- **Practicality beats purity**

Sources: [Red Hat Good Practices](https://redhat-cop.github.io/automation-good-practices/),
[Ansible Best Practices Guide](https://timgrt.github.io/Ansible-Best-Practices/)

---

## YAML Formatting

### Indentation

- Use **2 spaces** for indentation (never tabs)
- Be consistent throughout all files
- Configure editor to enforce this

### Boolean Values

```yaml
# CORRECT: Use true/false
enabled: true
disabled: false

# WRONG: Avoid yes/no or 1/0
enabled: yes    # Avoid
disabled: no    # Avoid
```

### Key-Value Spacing

```yaml
# CORRECT: Single space after colon
name: Install package

# WRONG: Multiple spaces or no space
name:Install package
name:  Install package
```

### File Extensions

- All Ansible YAML files: `.yml` (not `.yaml`, `.YML`)
- All Jinja2 templates: `.j2`

### Blank Lines

- One blank line before `vars`, `pre_tasks`, `roles`, `tasks`, `handlers`
- One blank line between each task

### Multi-line Strings

```yaml
# Literal block scalar (|) - preserves newlines
description: |
  This is a multi-line
  description that keeps
  each line separate.

# Folded block scalar (>) - folds newlines to spaces
description: >
  This is a long description
  that will be folded into
  a single line.

# Block chomping indicators
content: |+   # Keep trailing newlines
content: |-   # Strip trailing newlines
```

Sources: [Ansible YAML Syntax](https://docs.ansible.com/projects/ansible/latest/reference_appendices/YAMLSyntax.html),
[Jeff Geerling YAML Best Practices](https://www.jeffgeerling.com/blog/yaml-best-practices-ansible-playbooks-tasks)

---

## Naming Conventions

### Variables

```yaml
# CORRECT: snake_case
http_port: 8080
database_host: localhost
max_connections: 100

# WRONG: camelCase, kebab-case, or spaces
httpPort: 8080         # Wrong
database-host: localhost  # Wrong
```

### Role Names

- Use **lowercase with underscores** (snake_case) and **singular form**
- Examples: `nginx_server`, `database_backup`, `user_management`
- Note: Some style guides recommend kebab-case, but snake_case is more common
  and aligns with variable naming conventions

### Role Variables

- Prefix with role name to avoid collisions
- Use double underscore for internal variables

```yaml
# Public role variables (in defaults/main.yml)
nginx_port: 80
nginx_worker_processes: auto

# Internal variables (not meant for users)
__nginx_config_path: /etc/nginx
__nginx_temp_dir: /tmp/nginx
```

### Task Names

```yaml
# CORRECT: Action verb + description, sentence case
- name: Install nginx package
- name: Configure SSH daemon
- name: Create application user
- name: Ensure nginx configuration directory exists

# WRONG: Missing name, ALL CAPS, or comments instead of names
- apt:
    name: nginx  # Missing name!

- name: INSTALL NGINX  # Wrong: ALL CAPS

# Wrong: Don't use name as a comment
# Install nginx package
- apt:
    name: nginx
```

**Task naming rules:**

1. Always provide a `name:` parameter — no exceptions
2. Start with an action verb (install, configure, create, remove, ensure)
3. Use sentence case (capitalize first letter, lowercase rest except proper nouns)
4. Keep names unique within a play
5. Do not use templating in task names (e.g., `name: "Install {{ package }}"`)
6. The `name[casing]` ansible-lint rule enforces capitalization

Sources: [Ansible Lint name rules](https://docs.ansible.com/projects/lint/rules/name/),
[Ansible Junky Standards](https://www.ansiblejunky.com/blog/ansible-101-standards/)

---

## Task Structure

### Task Ordering

```yaml
- name: Ensure nginx package is installed
  ansible.builtin.package:
    name: nginx
    state: present
  become: true
  when: install_nginx | bool
  notify: Restart nginx
  tags:
    - nginx
    - packages
```

Recommended key order within a task:

1. `name`
2. Module name (FQCN)
3. Module parameters
4. `become` / `become_user`
5. `when`
6. `register`
7. `changed_when` / `failed_when`
8. `notify`
9. `tags`
10. `loop` / `with_*`

### One Task Per Action

```yaml
# CORRECT: Separate tasks for separate actions
- name: Install required packages
  ansible.builtin.package:
    name: "{{ item }}"
    state: present
  loop:
    - nginx
    - certbot

- name: Start nginx service
  ansible.builtin.service:
    name: nginx
    state: started

# WRONG: Combining unrelated actions
- name: Install and configure nginx  # Too broad
  block:
    # ... 20 tasks
```

---

## Variables

### Variable Precedence (Lowest to Highest)

1. Role defaults (`roles/x/defaults/main.yml`)
2. Inventory file or script group vars
3. `group_vars/all`
4. `group_vars/group_name`
5. Inventory file or script host vars
6. `host_vars/hostname`
7. Host facts / cached set_facts
8. Play vars
9. Play vars_prompt
10. Play vars_files
11. Role vars (`roles/x/vars/main.yml`)
12. Block vars
13. Task vars
14. include_vars
15. set_facts / registered vars
16. Role params
17. Include params
18. Extra vars (`-e`)

### Best Practices

```yaml
# Set defaults in role defaults
# roles/myapp/defaults/main.yml
myapp_port: 8080
myapp_user: myapp
myapp_log_level: info

# Override in group_vars for specific environments
# group_vars/production.yml
myapp_log_level: warn

# Use default filter for optional variables
port: "{{ custom_port | default(8080) }}"

# Use mandatory filter for required variables
database_password: "{{ db_pass | mandatory }}"
```

### Avoid Hardcoding

```yaml
# WRONG: Hardcoded values
- name: Create user
  ansible.builtin.user:
    name: john
    uid: 1001

# CORRECT: Use variables
- name: Create application user
  ansible.builtin.user:
    name: "{{ app_user }}"
    uid: "{{ app_user_uid }}"
```

Sources: [Ansible Variables Documentation](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_variables.html),
[Spacelift Ansible Variables Guide](https://spacelift.io/blog/ansible-variables)

---

## Conditionals

### Boolean Handling

```yaml
# For string booleans, use | bool filter
- name: Enable feature
  ansible.builtin.debug:
    msg: Feature enabled
  when: enable_feature | bool

# Direct boolean variables don't need filter
- name: Skip if disabled
  ansible.builtin.debug:
    msg: Running
  when: is_enabled  # Already a boolean
```

### Syntax

```yaml
# CORRECT: No {{ }} in when clauses
when: ansible_os_family == "Debian"

# WRONG: Using {{ }} in when
when: "{{ ansible_os_family == 'Debian' }}"  # Wrong!
```

### Multiple Conditions

```yaml
# Using 'and' (both must be true)
when:
  - ansible_os_family == "RedHat"
  - ansible_distribution_major_version | int >= 8

# Using 'or'
when: ansible_os_family == "Debian" or ansible_os_family == "Ubuntu"

# Complex logic
when: >
  (ansible_os_family == "RedHat" and ansible_distribution_major_version | int >= 8)
  or
  (ansible_os_family == "Debian" and ansible_distribution_major_version | int >= 10)
```

### Testing Variables

```yaml
# Check if defined
when: my_var is defined

# Check if undefined
when: my_var is not defined

# Check if empty
when: my_var | length > 0

# Check task results
when: previous_task is succeeded
when: previous_task is failed
when: previous_task is skipped
when: previous_task is changed
```

Sources: [Ansible Conditionals](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_conditionals.html)

---

## Loops

### Prefer `loop` Over `with_items`

```yaml
# CORRECT: Modern loop syntax
- name: Install packages
  ansible.builtin.package:
    name: "{{ item }}"
    state: present
  loop:
    - nginx
    - certbot
    - python3

# LEGACY: with_items (still works but prefer loop)
- name: Install packages
  ansible.builtin.package:
    name: "{{ item }}"
    state: present
  with_items:
    - nginx
    - certbot
```

### Loop Control

```yaml
- name: Create users
  ansible.builtin.user:
    name: "{{ item.name }}"
    groups: "{{ item.groups }}"
  loop: "{{ users }}"
  loop_control:
    label: "{{ item.name }}"  # Show only name in output
    pause: 2                   # Pause between iterations
    index_var: idx             # Access loop index
```

### Flattening Behavior

```yaml
# with_items flattens single level automatically
# loop does NOT flatten - add | flatten(1) if needed
- name: Install packages
  ansible.builtin.package:
    name: "{{ item }}"
  loop: "{{ package_lists | flatten(1) }}"
```

### Dictionary Loops

```yaml
- name: Create users with attributes
  ansible.builtin.user:
    name: "{{ item.key }}"
    comment: "{{ item.value.comment }}"
  loop: "{{ users | dict2items }}"
```

Sources: [Ansible Loops](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_loops.html)

---

## Handlers

### Basic Handler Pattern

```yaml
# In tasks
- name: Update nginx configuration
  ansible.builtin.template:
    src: nginx.conf.j2
    dest: /etc/nginx/nginx.conf
  notify: Restart nginx

# In handlers/main.yml
- name: Restart nginx
  ansible.builtin.service:
    name: nginx
    state: restarted
  become: true
```

### Handler Best Practices

1. **Unique names** — handlers must have globally unique names
2. **Order matters** — handlers run in definition order, not notification order
3. **Single execution** — handlers run once regardless of how many times notified

### Using `listen` for Handler Groups

```yaml
# Multiple handlers can listen to the same topic
handlers:
  - name: Restart nginx
    ansible.builtin.service:
      name: nginx
      state: restarted
    listen: web server changed

  - name: Clear nginx cache
    ansible.builtin.file:
      path: /var/cache/nginx
      state: absent
    listen: web server changed

# Task notifies the topic
- name: Update config
  ansible.builtin.template:
    src: nginx.conf.j2
    dest: /etc/nginx/nginx.conf
  notify: web server changed
```

### Flushing Handlers

```yaml
# Force handlers to run immediately
- name: Update critical config
  ansible.builtin.template:
    src: app.conf.j2
    dest: /etc/app/app.conf
  notify: Restart application

- name: Flush handlers
  ansible.builtin.meta: flush_handlers

- name: Run health check
  ansible.builtin.uri:
    url: http://localhost:8080/health
```

Sources: [Ansible Handlers](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_handlers.html)

---

## Error Handling

### Block / Rescue / Always

```yaml
- name: Deploy application
  block:
    - name: Download application
      ansible.builtin.get_url:
        url: "{{ app_url }}"
        dest: /tmp/app.tar.gz

    - name: Extract application
      ansible.builtin.unarchive:
        src: /tmp/app.tar.gz
        dest: /opt/app
        remote_src: true

  rescue:
    - name: Log deployment failure
      ansible.builtin.debug:
        msg: "Deployment failed: {{ ansible_failed_task.name }}"

    - name: Rollback to previous version
      ansible.builtin.copy:
        src: /opt/app.backup
        dest: /opt/app
        remote_src: true

  always:
    - name: Clean up temp files
      ansible.builtin.file:
        path: /tmp/app.tar.gz
        state: absent

    - name: Send notification
      ansible.builtin.uri:
        url: "{{ webhook_url }}"
        method: POST
        body: '{"status": "completed"}'
```

### failed_when and changed_when

```yaml
- name: Check if service is running
  ansible.builtin.command: systemctl is-active nginx
  register: nginx_status
  changed_when: false
  failed_when: nginx_status.rc not in [0, 3]

- name: Run migration
  ansible.builtin.command: ./migrate.sh
  register: migration_result
  changed_when: "'Applied' in migration_result.stdout"
  failed_when: migration_result.rc != 0 and 'already up to date' not in migration_result.stdout
```

### ignore_errors (Use Sparingly)

```yaml
# Only use when failure is expected and handled
- name: Check optional service
  ansible.builtin.command: systemctl status optional-service
  register: optional_check
  ignore_errors: true

- name: Configure if available
  ansible.builtin.template:
    src: optional.conf.j2
    dest: /etc/optional/config
  when: optional_check is succeeded
```

Sources: [Ansible Error Handling](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_error_handling.html),
[Ansible Blocks](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_blocks.html)

---

## Idempotency

### Core Principle

Running a playbook multiple times should produce the same result. The first run
makes changes; subsequent runs report "ok" with no changes.

### Command/Shell Module Idempotency

```yaml
# WRONG: Always reports changed
- name: Create directory
  ansible.builtin.command: mkdir -p /opt/myapp

# CORRECT: Use appropriate module
- name: Create directory
  ansible.builtin.file:
    path: /opt/myapp
    state: directory

# If command/shell is necessary, use creates/removes
- name: Initialize database
  ansible.builtin.command: /opt/db/init.sh
  args:
    creates: /opt/db/.initialized

# Or use check-then-act pattern
- name: Check current hostname
  ansible.builtin.command: hostname
  register: current_hostname
  changed_when: false

- name: Set hostname
  ansible.builtin.command: hostnamectl set-hostname "{{ target_hostname }}"
  when: current_hostname.stdout != target_hostname
```

### Idempotent Patterns

```yaml
# Package management - already idempotent
- name: Ensure nginx is installed
  ansible.builtin.package:
    name: nginx
    state: present  # Not 'latest' unless you want updates

# File operations - use state
- name: Ensure config exists
  ansible.builtin.copy:
    src: app.conf
    dest: /etc/app/app.conf

# Service management
- name: Ensure service is running
  ansible.builtin.service:
    name: nginx
    state: started
    enabled: true
```

Sources: [Ansible Idempotency](https://reintech.io/blog/writing-idempotent-tasks-in-ansible)

---

## Role Structure

### Standard Directory Layout

```
roles/
└── my_role/
    ├── defaults/
    │   └── main.yml      # Default variables (lowest precedence)
    ├── files/
    │   └── app.conf      # Static files for copy module
    ├── handlers/
    │   └── main.yml      # Handler definitions
    ├── meta/
    │   └── main.yml      # Role metadata, dependencies
    ├── tasks/
    │   └── main.yml      # Main task list
    ├── templates/
    │   └── config.j2     # Jinja2 templates
    ├── tests/
    │   ├── inventory
    │   └── test.yml      # Test playbook
    ├── vars/
    │   └── main.yml      # Role variables (high precedence)
    └── README.md         # Documentation
```

### Role Design Principles

1. **Single responsibility** — one role, one purpose
2. **Self-contained** — include all necessary tasks, templates, files
3. **Configurable** — expose variables in defaults/
4. **Documented** — README with examples

### Role Dependencies (meta/main.yml)

```yaml
---
galaxy_info:
  author: Your Name
  description: Role description
  license: MIT
  min_ansible_version: "2.10"
  platforms:
    - name: Ubuntu
      versions:
        - focal
        - jammy
    - name: EL
      versions:
        - "8"
        - "9"

dependencies:
  - role: common
  - role: firewall
    vars:
      firewall_allowed_ports:
        - 80
        - 443
```

### Galaxy Versioning

- Use strict **X.Y.Z** format (e.g., 1.0.0, 2.1.3)
- Follow semantic versioning

Sources: [Ansible Roles](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_reuse_roles.html),
[Galaxy Developer Guide](https://docs.ansible.com/projects/ansible/latest/galaxy/dev_guide.html)

---

## Collections and FQCN

### Always Use Fully Qualified Collection Names

```yaml
# CORRECT: FQCN
- name: Install package
  ansible.builtin.package:
    name: nginx
    state: present

- name: Gather AWS EC2 facts
  amazon.aws.ec2_instance_info:
    region: us-east-1

# WRONG: Short module names (deprecated)
- name: Install package
  package:              # Ambiguous!
    name: nginx

# WRONG: Using collections keyword
collections:
  - amazon.aws          # Avoid this pattern
```

### Common Collection Namespaces

- `ansible.builtin` — core Ansible modules
- `ansible.posix` — POSIX modules (acl, cron, etc.)
- `ansible.netcommon` — network common modules
- `community.general` — community maintained modules
- `community.mysql` — MySQL modules
- `community.postgresql` — PostgreSQL modules
- `amazon.aws` — AWS modules
- `google.cloud` — GCP modules
- `azure.azcollection` — Azure modules

### Installing Collections

```yaml
# requirements.yml
---
collections:
  - name: community.general
    version: ">=6.0.0"
  - name: amazon.aws
    version: ">=5.0.0"

# Install: ansible-galaxy collection install -r requirements.yml
```

Sources: [Ansible FQCN Lint Rule](https://docs.ansible.com/projects/lint/rules/fqcn/),
[Using Collections](https://docs.ansible.com/projects/ansible/latest/collections_guide/collections_using_playbooks.html)

---

## Inventory Best Practices

### Directory Structure

```
inventory/
├── production/
│   ├── hosts.yml
│   ├── group_vars/
│   │   ├── all.yml
│   │   ├── webservers.yml
│   │   └── databases.yml
│   └── host_vars/
│       └── web01.example.com.yml
└── staging/
    ├── hosts.yml
    ├── group_vars/
    │   └── all.yml
    └── host_vars/
```

### YAML Inventory Format

```yaml
# inventory/production/hosts.yml
---
all:
  children:
    webservers:
      hosts:
        web01.example.com:
        web02.example.com:
      vars:
        http_port: 80
    databases:
      hosts:
        db01.example.com:
          mysql_port: 3306
```

### Variable Organization

```yaml
# group_vars/all.yml - Common defaults
---
ntp_servers:
  - 0.pool.ntp.org
  - 1.pool.ntp.org

# group_vars/webservers.yml - Web server specific
---
nginx_worker_processes: auto
nginx_worker_connections: 1024

# host_vars/web01.example.com.yml - Host specific
---
nginx_worker_processes: 4  # Override for this host
```

### Best Practices

1. Use version control for inventory
2. Separate environments (production, staging, development)
3. Use dynamic inventory for cloud providers
4. Set common defaults in `group_vars/all.yml`
5. Host vars override group vars

Sources: [Ansible Inventory Guide](https://docs.ansible.com/projects/ansible/latest/inventory_guide/intro_inventory.html)

---

## Security

### Ansible Vault for Secrets

```bash
# Create encrypted file
ansible-vault create secrets.yml

# Encrypt existing file
ansible-vault encrypt vars/credentials.yml

# Edit encrypted file
ansible-vault edit secrets.yml

# Run playbook with vault password
ansible-playbook site.yml --ask-vault-pass
ansible-playbook site.yml --vault-password-file ~/.vault_pass
```

### no_log for Sensitive Tasks

```yaml
- name: Set database password
  ansible.builtin.mysql_user:
    name: app_user
    password: "{{ db_password }}"
    priv: "app_db.*:ALL"
  no_log: true
```

### Privilege Escalation (become)

```yaml
# At play level
- hosts: webservers
  become: true
  become_user: root
  tasks:
    - name: Install package
      ansible.builtin.package:
        name: nginx

# At task level (preferred for minimal privilege)
- name: Read user file
  ansible.builtin.slurp:
    src: /home/app/config
  become: true
  become_user: app
```

### Security Best Practices

1. **Never commit secrets** — use Vault
2. **Minimal become** — only escalate when necessary
3. **Avoid shell/command** — prefer idempotent modules
4. **Validate input** — especially from external sources
5. **Secure file permissions** — use mode parameter

```yaml
- name: Create sensitive config
  ansible.builtin.template:
    src: database.conf.j2
    dest: /etc/app/database.conf
    owner: app
    group: app
    mode: "0600"  # Only owner can read
```

Sources: [Ansible Vault](https://docs.ansible.com/ansible/latest/vault_guide/index.html),
[Privilege Escalation](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_privilege_escalation.html)

---

## Performance

### Fact Gathering

```yaml
# Disable if not needed
- hosts: webservers
  gather_facts: false
  tasks:
    - name: Quick task
      ansible.builtin.ping:

# Gather only what you need
- hosts: webservers
  gather_subset:
    - network
    - virtual
```

### Fact Caching

```ini
# ansible.cfg
[defaults]
gathering = smart
fact_caching = jsonfile
fact_caching_connection = /tmp/ansible_facts
fact_caching_timeout = 86400
```

### SSH Pipelining

```ini
# ansible.cfg
[ssh_connection]
pipelining = True
```

### Parallelism

```ini
# ansible.cfg
[defaults]
forks = 20  # Default is 5
```

### Strategy

```yaml
# Free strategy - tasks run as fast as possible
- hosts: webservers
  strategy: free
  tasks:
    - name: Independent task
      ansible.builtin.package:
        name: nginx
```

Sources: [Ansible Tuning](https://docs.openstack.org/kolla-ansible/latest/user/ansible-tuning.html),
[Spacelift Best Practices](https://spacelift.io/blog/ansible-best-practices)

---

## Testing

### Ansible Lint

```bash
# Install
pip install ansible-lint

# Run on playbook
ansible-lint playbook.yml

# Run on role
ansible-lint roles/my_role/
```

### Lint Configuration (.ansible-lint)

```yaml
---
profile: production

skip_list:
  - yaml[line-length]  # With comment explaining why

warn_list:
  - experimental

exclude_paths:
  - .cache/
  - .github/
```

### Molecule for Role Testing

```bash
# Install
pip install molecule molecule-docker

# Initialize role with Molecule
molecule init role my_role

# Run tests
molecule test

# Develop interactively
molecule converge
molecule verify
molecule destroy
```

### Check Mode (Dry Run)

```bash
# Preview changes without applying
ansible-playbook site.yml --check

# With diff to see changes
ansible-playbook site.yml --check --diff
```

### Assert Module for Validation

```yaml
- name: Validate configuration
  ansible.builtin.assert:
    that:
      - app_port > 1024
      - app_user is defined
      - app_user | length > 0
    fail_msg: "Invalid configuration: port must be > 1024 and user must be defined"
    success_msg: "Configuration validated successfully"
    quiet: true  # Only show on failure
```

Sources: [Ansible Lint](https://docs.ansible.com/projects/lint/),
[Molecule Documentation](https://molecule.readthedocs.io/)

---

## Common Anti-patterns

### 1. Using shell/command When a Module Exists

```yaml
# WRONG
- name: Install package
  ansible.builtin.shell: apt-get install nginx -y

# CORRECT
- name: Install package
  ansible.builtin.apt:
    name: nginx
    state: present
```

### 2. Not Using FQCN

```yaml
# WRONG
- name: Copy file
  copy:
    src: file.txt
    dest: /tmp/file.txt

# CORRECT
- name: Copy file
  ansible.builtin.copy:
    src: file.txt
    dest: /tmp/file.txt
```

### 3. Hardcoding Values

```yaml
# WRONG
- name: Create user
  ansible.builtin.user:
    name: admin
    uid: 1000

# CORRECT
- name: Create application user
  ansible.builtin.user:
    name: "{{ app_user }}"
    uid: "{{ app_user_uid }}"
```

### 4. Missing Task Names

```yaml
# WRONG
- ansible.builtin.package:
    name: nginx
    state: present

# CORRECT
- name: Install nginx web server
  ansible.builtin.package:
    name: nginx
    state: present
```

### 5. Using `import_tasks` with Loops

```yaml
# WRONG - import_tasks doesn't support loops
- import_tasks: user.yml
  loop: "{{ users }}"

# CORRECT - use include_tasks for loops
- include_tasks: user.yml
  loop: "{{ users }}"
```

### 6. Storing Secrets in Plain Text

```yaml
# WRONG
vars:
  database_password: "supersecret123"

# CORRECT - Use Vault
vars_files:
  - vault/secrets.yml  # Encrypted with ansible-vault
```

### 7. Ignoring Errors Without Handling

```yaml
# WRONG - Silent failure
- name: Run script
  ansible.builtin.command: /opt/script.sh
  ignore_errors: true

# CORRECT - Handle the failure
- name: Run script
  ansible.builtin.command: /opt/script.sh
  register: script_result
  failed_when: false

- name: Handle script failure
  ansible.builtin.debug:
    msg: "Script failed with: {{ script_result.stderr }}"
  when: script_result.rc != 0
```

### 8. Not Making Tasks Idempotent

```yaml
# WRONG - Runs every time
- name: Add line to file
  ansible.builtin.shell: echo "config=value" >> /etc/app.conf

# CORRECT - Idempotent
- name: Configure application
  ansible.builtin.lineinfile:
    path: /etc/app.conf
    line: "config=value"
```

### 9. Overusing `become: true`

```yaml
# WRONG - Play-level become when not all tasks need it
- hosts: webservers
  become: true
  tasks:
    - name: Check free space  # Doesn't need root
      ansible.builtin.command: df -h
    - name: Install package   # Needs root
      ansible.builtin.package:
        name: nginx

# CORRECT - Task-level become
- hosts: webservers
  tasks:
    - name: Check free space
      ansible.builtin.command: df -h
      changed_when: false
    - name: Install package
      ansible.builtin.package:
        name: nginx
      become: true
```

### 10. Not Using Handlers for Restarts

```yaml
# WRONG - Always restarts
- name: Update config
  ansible.builtin.template:
    src: app.conf.j2
    dest: /etc/app/app.conf

- name: Restart service
  ansible.builtin.service:
    name: app
    state: restarted

# CORRECT - Only restarts on change
- name: Update config
  ansible.builtin.template:
    src: app.conf.j2
    dest: /etc/app/app.conf
  notify: Restart app

handlers:
  - name: Restart app
    ansible.builtin.service:
      name: app
      state: restarted
```

Sources: [Ansible Anti-patterns](https://medium.com/@william.wheeler_87363/ansible-antipatterns-9606bccad018)

---

## Tags Best Practices

### Special Tags

- `always` — runs unless explicitly skipped with `--skip-tags always`
- `never` — skipped unless explicitly requested with `--tags never`

```yaml
- name: Always run health check
  ansible.builtin.uri:
    url: http://localhost/health
  tags:
    - always
    - health

- name: Debug task (only when requested)
  ansible.builtin.debug:
    var: all_vars
  tags:
    - never
    - debug
```

### Tag Best Practices

1. Always add a specific named tag alongside `always` or `never`
2. Use consistent tag names across playbooks
3. Document tag meanings in README

```bash
# Run only specific tags
ansible-playbook site.yml --tags "deploy,config"

# Skip specific tags
ansible-playbook site.yml --skip-tags "debug"

# List available tags
ansible-playbook site.yml --list-tags
```

Sources: [Ansible Tags](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_tags.html)

---

## Register and Return Values

### Common Return Values

```yaml
- name: Run command
  ansible.builtin.command: whoami
  register: result

# Access return values
- name: Show results
  ansible.builtin.debug:
    msg: |
      stdout: {{ result.stdout }}
      stderr: {{ result.stderr }}
      rc: {{ result.rc }}
      changed: {{ result.changed }}
      stdout_lines: {{ result.stdout_lines }}
```

### Best Practices

```yaml
# Use changed_when with register for commands
- name: Check status
  ansible.builtin.command: systemctl is-active nginx
  register: nginx_status
  changed_when: false
  failed_when: nginx_status.rc not in [0, 3]

# Use stdout_lines for multi-line output
- name: List files
  ansible.builtin.command: ls /opt
  register: files_list
  changed_when: false

- name: Process each file
  ansible.builtin.debug:
    msg: "Found: {{ item }}"
  loop: "{{ files_list.stdout_lines }}"
```

Sources: [Ansible Return Values](https://docs.ansible.com/ansible/latest/reference_appendices/common_return_values.html)

---

## Jinja2 Templates

### Template Best Practices

```jinja2
{# templates/app.conf.j2 #}
# Managed by Ansible - DO NOT EDIT
# Generated: {{ ansible_date_time.iso8601 }}
# Host: {{ inventory_hostname }}

[application]
port = {{ app_port | default(8080) }}
host = {{ app_host | mandatory }}
debug = {{ app_debug | default(false) | lower }}

{% if app_features is defined %}
[features]
{% for feature in app_features %}
{{ feature.name }} = {{ feature.enabled | lower }}
{% endfor %}
{% endif %}
```

### Useful Filters

```yaml
# Default values
"{{ variable | default('fallback') }}"

# Type conversion
"{{ string_number | int }}"
"{{ value | bool }}"

# String operations
"{{ name | upper }}"
"{{ name | lower }}"
"{{ name | capitalize }}"
"{{ list | join(', ') }}"

# List operations
"{{ list | first }}"
"{{ list | last }}"
"{{ list | unique }}"
"{{ list | sort }}"

# JSON/YAML
"{{ dict | to_json }}"
"{{ dict | to_nice_yaml }}"

# Path operations
"{{ path | basename }}"
"{{ path | dirname }}"

# Password hashing
"{{ password | password_hash('sha512') }}"
```

Sources: [Ansible Templating](https://docs.ansible.com/ansible/latest/playbook_guide/playbooks_templating.html),
[Ansible Filters](https://docs.ansible.com/ansible/latest/playbook_guide/playbooks_filters.html)

---

## include_tasks vs import_tasks

### Key Differences

| Feature | `import_tasks` | `include_tasks` |
|---------|----------------|-----------------|
| Processing | Static (parse time) | Dynamic (runtime) |
| Loops | Not supported | Supported |
| Conditionals | Applied to each task | Applied to include itself |
| Tags | Inherited by imported tasks | Not inherited |
| `--list-tasks` | Shows tasks | Doesn't show tasks |

### When to Use Each

```yaml
# Use import_tasks for static includes
- import_tasks: common.yml

# Use include_tasks for dynamic/looped includes
- include_tasks: user.yml
  loop: "{{ users }}"
  loop_control:
    loop_var: user

# Use include_tasks with conditionals
- include_tasks: "{{ ansible_os_family | lower }}.yml"
```

### General Recommendation

Prefer `include_tasks` unless you specifically need static parsing behavior.
The limitations of `import_tasks` (no loops, no dynamic file names) are more
impactful than the minor overhead of dynamic includes.

Sources: [Ansible Include vs Import](https://docs.ansible.com/projects/ansible/2.9/user_guide/playbooks_reuse_includes.html)

---

## References

### Companion Document

See also: [ansible-standards.md](./ansible-standards.md) for detailed coverage of:

- Collection structure and `galaxy.yml` requirements
- Role `argument_specs.yml` for input validation
- Molecule test configuration and multi-platform testing
- `.yamllint` configuration

### Official Documentation

- [Ansible Documentation](https://docs.ansible.com/)
- [Ansible Lint Rules](https://docs.ansible.com/projects/lint/rules/)
- [Ansible Galaxy](https://galaxy.ansible.com/)

### Best Practices Guides

- [Red Hat Automation Good Practices](https://redhat-cop.github.io/automation-good-practices/)
- [Ansible Best Practices (timgrt)](https://timgrt.github.io/Ansible-Best-Practices/)
- [Spacelift Ansible Best Practices](https://spacelift.io/blog/ansible-best-practices)

### Style Guides

- [Ansible Style Guide (metno)](https://github.com/metno/ansible-style-guide)
- [Ansible Junky Standards](https://www.ansiblejunky.com/blog/ansible-101-standards/)

### Testing

- [Molecule Documentation](https://molecule.readthedocs.io/)
- [Ansible Lint Documentation](https://docs.ansible.com/projects/lint/)

---

_Last updated: 2026-02-06_
