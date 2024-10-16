# Docker Customization

If the container you run `gonb` needs some custom initialization, and you don't want to simply edit the dockerfile and
create your own docker, just create the file `autostart.sh` in the directory mounted under `/notebooks` in the container,
owned by `root` and with executable permissions, and it will be executed at start up of the container by default.

This allows you to download/install databases, or set up credentials, etc.

Example of an `autostart.sh` that:

- Sets the timezone
- Installs [`nats`](github.com/nats-io/natscli/) for the jupyer user (given by `$NB_USER`).

```
# Set the German timezone (so time.Now() returns German time)
apt-get install -y tzdata
ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime

# some locale magic to make "date" answer with German format
echo 'de_DE.UTF-8 UTF-8' >> /etc/locale.gen
locale-gen
echo 'LC_ALL="de_DE.utf8"' > /etc/default/locale
export LC_ALL="de_DE.UTF-8"
dpkg-reconfigure locales

# check that it works
date

# Installing Go tools for $NB_USER.
su -l "$NB_USER" -c "go install github.com/nats-io/natscli/nats@latest"
```

More details in the `Dockerfile` and in the small start script `cmd/check_and_run_autostart.sh`.
