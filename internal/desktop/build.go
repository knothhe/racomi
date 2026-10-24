package desktop

import "racomi/internal/buildinfo"

func (s *Service) GetBuildInfo() buildinfo.Info { return buildinfo.Current() }
