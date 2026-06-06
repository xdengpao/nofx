use serde::{Deserialize, Serialize};
use crate::center::Center;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum TrendType {
    #[serde(rename = "up_trend")]
    UpTrend,
    #[serde(rename = "down_trend")]
    DownTrend,
    #[serde(rename = "consolidation")]
    Consolidation,
    #[serde(rename = "unknown")]
    Unknown,
}

/// 根据中枢序列判断走势类型
pub fn classify_trend(centers: &[Center]) -> TrendType {
    if centers.len() < 2 {
        if centers.len() == 1 {
            return TrendType::Consolidation;
        }
        return TrendType::Unknown;
    }
    let first = &centers[0];
    let last = &centers[centers.len() - 1];
    if last.zd > first.zg {
        TrendType::UpTrend
    } else if last.zg < first.zd {
        TrendType::DownTrend
    } else {
        TrendType::Consolidation
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn c(zg: f64, zd: f64) -> Center {
        Center { id: 0, zg, zd, high: zg + 5.0, low: zd - 5.0, segments: vec![], start_time: 0, end_time: 0 }
    }

    #[test]
    fn test_up_trend() {
        let centers = vec![c(100.0, 90.0), c(120.0, 110.0)];
        assert_eq!(classify_trend(&centers), TrendType::UpTrend);
    }

    #[test]
    fn test_down_trend() {
        let centers = vec![c(100.0, 90.0), c(80.0, 70.0)];
        assert_eq!(classify_trend(&centers), TrendType::DownTrend);
    }

    #[test]
    fn test_consolidation() {
        let centers = vec![c(100.0, 90.0), c(105.0, 85.0)];
        assert_eq!(classify_trend(&centers), TrendType::Consolidation);
    }
}
