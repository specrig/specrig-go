// The container hands the outbox to whatever asks for a Publisher.
package wire

import "gitlab.com/gorib/waffle/tools/outbox"

var Publisher outbox.Publisher
