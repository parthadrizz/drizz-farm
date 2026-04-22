# frozen_string_literal: true

# Homebrew formula for drizz-farm.
#
# This file is templated: placeholders in ALL_CAPS are replaced by the
# release script (`make release`) with the actual version, URL, and SHA256.
# The generated file is then committed to the `drizz-dev/homebrew-tap` repo
# so users can: `brew install drizz-dev/tap/drizz-farm`.
class DrizzFarm < Formula
  desc "Self-hosted Android emulator pool — a device lab on your own Mac"
  homepage "https://drizz.ai"
  version "VERSION_PLACEHOLDER"
  license "Apache-2.0"

  # Universal macOS binary. Works on both Apple Silicon and Intel.
  on_macos do
    url "https://github.com/parthadrizz/drizz-farm/releases/download/#{version}/drizz-farm-#{version}-darwin-universal.tar.gz"
    sha256 "SHA256_PLACEHOLDER"
  end

  # Users should install Android Studio or the command-line tools separately;
  # drizz-farm detects the SDK at `drizz-farm setup` time. We don't list it as
  # a dependency because many users already have it via Android Studio.

  def install
    bin.install "drizz-farm-darwin-universal" => "drizz-farm"
  end

  def caveats
    <<~EOS
      drizz-farm is installed. Finish setup with:

        drizz-farm setup        # detect Android SDK, install as a background service
        drizz-farm start        # or start manually

      Dashboard: http://$(hostname).local:9401

      The setup wizard also installs drizz-farm as a launchd service
      so it auto-starts on login. You can manage that later with:

        drizz-farm daemon install    # enable auto-start
        drizz-farm daemon uninstall  # disable auto-start
        drizz-farm daemon status
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/drizz-farm version")
  end
end
