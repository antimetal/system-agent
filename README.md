# Antimetal Agent

Ahoy there, ye scurvy dogs! This here be the mystical contraption what hooks yer infrastructure up to the grand [Antimetal](https://antimetal.com) platform, savvy?

## Contributin' to the Cause

If ye be wantin' to throw yer lot in with us and contribute, best be steerin' yer course to our [DEVELOPING](./DEVELOPING.md) scrolls, ya landlubber!

## Helm Charts (The Treasure Maps)

The helm charts fer this fine vessel of an Agent be stashed away in the [antimetal/helm-charts](https://github.com/antimetal/helm-charts) treasure chest, arr!

## Docker Images (Bottles o' Code)

We be bottlin' up fresh Docker images with every new release and floatin' 'em over to [DockerHub](https://hub.docker.com/r/antimetal/agent), we do!
We be buildin' fer both linux/amd64 and linux/arm64, so all ye different ships can sail with us, har har!

## When Ye Need a Hand
If ye find yerself three sheets to the wind and needin' help, drop us a message in a bottle (GitHub Issue).
For them fancy merchant vessels needin' commercial support, send a parrot to support@antimetal.com, ye hear?

## The Code of the Sea (License)

Listen up, ye bilge rats! This here project be split like a ship's plunder:

- **Userspace components** (`/cmd`, `/internal`, `/pkg`): Protected by the PolyForm Shield, like a proper privateer's letter of marque!
- **eBPF programs** (`/ebpf`): Sailin' under the GPL-2.0-only flag, as required by the King's Navy (kernel)!

The eBPF programs be needin' that GPL license on account of them kernel helper functions bein' GPL-only - that's just how the sea laws work, savvy? 
Every proper ship with both userspace and eBPF cargo does it this way, arr!

If ye be just a regular sailor usin' this code, ye can pretty much do whatever tickles yer fancy with the userspace bits under PolyForm Shield.
Check the License FAQ if ye need more learnin' about them PolyForm waters, matey!
